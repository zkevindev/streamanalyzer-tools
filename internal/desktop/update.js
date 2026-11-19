'use strict';

(() => {
  const el = id => document.getElementById(id);
  const token = document.querySelector('meta[name="update-token"]').content;
  const dialog = el('update-dialog');
  const nativeDialog = typeof dialog.showModal === 'function';
  const openButton = el('open-updates');
  const installButton = el('update-install');
  const closeButton = el('update-close');
  const statuses = {
    idle: '准备检查更新',
    checking: '正在检查 GitHub 正式版…',
    current: '当前已是最新正式版',
    available: '可以下载安装包',
    downloading: '正在下载安装包…',
    verifying: '正在校验 SHA-256…',
    preparing: '正在准备已校验的安装包…',
    ready: '安装包已通过 SHA-256 校验',
    restarting: '正在退出并应用更新…',
  };
  const active = new Set(['checking', 'downloading', 'verifying', 'preparing', 'restarting']);
  let state;
  let sending = false;
  let confirmVersion = '';
  let pollID;
  let revision = 0;
  let requestError = '';

  const megabytes = bytes => (bytes / (1024 * 1024)).toFixed(1) + ' MiB';

  function render() {
    if (!state) return;
    const {current, release, phase, received} = state;
    const blocked = sending || active.has(phase);
    el('desktop-version').textContent = current === 'dev' ? '开发构建' : current;
    openButton.textContent = release ? (phase === 'ready' ? '更新已就绪' : '发现新版 ' + release.version) : '检查更新';
    el('update-version').textContent = '当前版本 ' + current + (release ? ' → ' + release.version + ' · ' + megabytes(release.size) : '');
    const notices = Boolean(release?.notes);
    el('update-note-label').hidden = el('update-notes').hidden = !notices;
    if (el('update-notes').textContent !== (release?.notes || '')) el('update-notes').textContent = release?.notes || '';

    const progress = el('update-progress');
    progress.hidden = !['downloading', 'verifying', 'preparing'].includes(phase);
    const percent = release ? Math.min(100, Math.floor(received * 100 / release.size)) : 0;
    progress.value = percent;
    const status = el('update-status');
    status.textContent = requestError || state.error || (phase === 'downloading' && release
      ? percent + '% · ' + megabytes(received) + ' / ' + megabytes(release.size) : statuses[phase] || '');
    status.classList.toggle('error', Boolean(requestError || state.error));

    const confirming = phase === 'ready' && confirmVersion === release?.version;
    el('update-confirm-copy').hidden = !confirming;
    el('update-confirm-copy').textContent = confirming ? '确认安装 ' + confirmVersion + ' 并重启？进行中的采样和模拟会话将结束。' : '';
    el('update-check').hidden = confirming || phase === 'ready' || phase === 'restarting';
    el('update-check').disabled = blocked || phase === 'disabled';
    el('update-download').hidden = phase !== 'available';
    el('update-download').disabled = blocked;
    installButton.hidden = phase !== 'ready';
    installButton.disabled = blocked;
    installButton.textContent = confirming ? '确认安装并重启' : '安装并重启';
    closeButton.disabled = phase === 'restarting';
    closeButton.textContent = confirming ? '取消安装' : '关闭';
  }

  async function request(action, data) {
    const res = await fetch('/desktop-update' + (action ? '/' + action : ''), {
      method: action ? 'POST' : 'GET',
      cache: 'no-store',
      headers: {'X-StreamAnalyzer-Update':token, ...(data && {'Content-Type':'application/json'})},
      ...(data && {body:JSON.stringify(data)}),
    });
    const result = await res.json();
    if (!res.ok) throw new Error(result.error || '软件更新请求失败');
    return result;
  }

  function schedulePoll() {
    clearTimeout(pollID);
    if (active.has(state?.phase)) pollID = setTimeout(refresh, 300);
  }

  async function refresh() {
    const now = revision;
    try {
      const status = await request();
      if (now === revision && !sending) {
        state = status;
        requestError = '';
        render();
      }
    } catch (error) {
      if (now === revision && !sending) {
        requestError = error.message;
        if (!state) state = {current:'—', phase:'error'};
        render();
      }
    } finally {
      if (now === revision && !sending) schedulePoll();
    }
  }

  async function start(action, payload) {
    if (sending || active.has(state?.phase)) return;
    ++revision;
    sending = true;
    requestError = '';
    render();
    try {
      state = await request(action, payload);
    } catch (error) {
      requestError = error.message;
    } finally {
      sending = false;
      render();
      schedulePoll();
    }
  }

  function check() {
    if (!['disabled', 'ready'].includes(state?.phase)) void start('check');
  }

  function closeDialog() {
    if (state?.phase === 'restarting') return;
    if (confirmVersion) {
      confirmVersion = '';
      render();
      installButton.focus();
      return;
    }
    if (nativeDialog) dialog.close();
    else {
      dialog.removeAttribute('open');
      el('update-backdrop').hidden = true;
      el('workbench').removeAttribute('aria-hidden');
      el('workbench').removeAttribute('tabindex');
    }
    openButton.focus();
  }

  openButton.addEventListener('click', () => {
    if (dialog.hasAttribute('open')) return;
    if (nativeDialog) dialog.showModal();
    else {
      dialog.setAttribute('open', '');
      el('update-backdrop').hidden = false;
      el('workbench').setAttribute('aria-hidden', 'true');
      el('workbench').tabIndex = -1;
    }
    closeButton.focus();
    if (!state?.checked_at && !state?.release) check();
  });
  closeButton.addEventListener('click', closeDialog);
  dialog.addEventListener('cancel', event => {
    event.preventDefault();
    closeDialog();
  });
  dialog.addEventListener('keydown', event => {
    if (nativeDialog) return;
    if (event.key === 'Escape') closeDialog();
    if (event.key !== 'Tab') return;
    const controls = [...dialog.querySelectorAll('button:not(:disabled):not([hidden]), pre:not([hidden])')];
    if (event.shiftKey && document.activeElement === controls[0]) {
      event.preventDefault();
      controls.at(-1)?.focus();
    } else if (!event.shiftKey && document.activeElement === controls.at(-1)) {
      event.preventDefault();
      controls[0]?.focus();
    }
  });
  el('update-check').addEventListener('click', check);
  el('update-download').addEventListener('click', () => {
    if (state?.release) void start('download', {version:state.release.version});
  });
  installButton.addEventListener('click', () => {
    if (state?.phase !== 'ready') return;
    const version = state.release.version;
    if (confirmVersion === version) {
      confirmVersion = '';
      void start('install', {version, confirmed:true});
    } else {
      confirmVersion = version;
      render();
      closeButton.focus();
    }
  });

  void refresh().then(check);
  setInterval(() => { if (!dialog.hasAttribute('open')) check(); }, 6 * 60 * 60 * 1000);
})();
