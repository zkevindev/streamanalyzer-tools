'use strict';
(() => {
    const root = document.querySelector('#onvif-discovery-root');
    if (!root) return;
    root.innerHTML = `<details class="panel onvif-discovery" id="onvif-discovery">
      <summary>ONVIF 摄像头与 NVR <span>发现局域网设备，选择媒体 Profile 并回填采样地址</span></summary>
      <div class="onvif-content">
        <form id="onvif-search" class="onvif-grid">
          <div><label for="onvif-interface">本机网卡 / IPv4</label><select id="onvif-interface" required><option value="">正在读取网卡…</option></select></div>
          <div><label for="onvif-seconds">搜索时长</label><select id="onvif-seconds"><option value="3">3 秒</option><option value="5">5 秒</option><option value="10">10 秒</option></select></div>
          <div class="onvif-controls"><button id="onvif-search-button" type="submit">搜索设备</button><button id="onvif-stop" type="button" class="secondary" disabled>停止搜索</button></div>
        </form>
        <p class="onvif-note muted">WS-Discovery 仅搜索所选网卡子网内的 IPv4 设备。跨网段、组播被禁用或未发现设备时，可手动填写设备服务地址。发现结果只保留在当前应用内存。</p>
        <p id="onvif-status" role="status" aria-live="polite">选择网卡后搜索设备，或使用手动服务地址。</p>
        <div id="onvif-devices" class="scroll onvif-devices" role="region" tabindex="0" aria-label="ONVIF 发现设备，可横向滚动" hidden></div>
        <form id="onvif-query">
          <div class="onvif-query">
            <fieldset><legend>设备服务</legend><p id="onvif-selected" class="muted">尚未选择设备</p>
              <label class="onvif-inline"><input id="onvif-manual" type="checkbox"> 手动填写 ONVIF 服务地址</label>
              <div id="onvif-manual-fields" hidden><label for="onvif-service-url">Device 服务地址</label><input id="onvif-service-url" type="url" placeholder="http://192.168.1.10/onvif/device_service" autocomplete="off"></div>
            </fieldset>
            <fieldset><legend>ONVIF 鉴权（仅当前查询）</legend>
              <label for="onvif-username">用户名</label><input id="onvif-username" maxlength="128" autocomplete="off">
              <label for="onvif-password">密码</label><input id="onvif-password" type="password" maxlength="256" autocomplete="new-password">
              <p class="onvif-note muted">使用 UsernameToken PasswordDigest。凭据不写入历史记录、证据包或浏览器存储。</p>
            </fieldset>
          </div>
          <button type="submit" id="onvif-read-profiles" class="secondary">读取设备 Profile</button>
        </form>
        <section id="onvif-profiles" hidden aria-label="ONVIF 媒体 Profile">
          <p id="onvif-clock" class="onvif-note muted"></p>
          <div class="onvif-profile"><div><label for="onvif-profile">媒体 Profile</label><select id="onvif-profile"></select></div>
          <div><label for="onvif-transport">RTSP 传输</label><select id="onvif-transport"><option value="tcp">TCP 交织</option><option value="udp">UDP 单播</option></select></div></div>
          <label class="onvif-inline"><input id="onvif-copy-credentials" type="checkbox"> 将本次 ONVIF 凭据复制到同主机 RTSP 地址（默认不复制）</label>
          <button type="button" id="onvif-prefill">获取地址并回填采样</button>
          <p class="onvif-note muted">回填后请核对播放地址与传输方式，再点击“采样并保存原始流”。不会自动开始拉流。</p>
        </section>
      </div></details>`;
    const node = id => document.getElementById(id), base = '/api/diagnostics/onvif';
    let job, selected, selection, poll, operation, generation = 0, targetRevision = 0, pollRequest = 0;
    const status = (text, error = false) => { node('onvif-status').textContent = text; node('onvif-status').dataset.error = String(error); };
    async function request(path, body, signal) {
        const response = await fetch(base + path, body === undefined ? {signal} : {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body), signal});
        const value = await response.json();
        if (!response.ok) throw new Error(`${value.error || 'ONVIF 请求失败'}${value.stage ? `（${value.stage}）` : ''}`);
        return value;
    }
    function clearProfiles() { targetRevision++; operation?.abort(); selection = undefined; node('onvif-profiles').hidden = true; node('onvif-copy-credentials').checked = false; }
    function credentials() { return {username: node('onvif-username').value, password: node('onvif-password').value}; }
    function renderDevices(value) {
        const area = node('onvif-devices'); area.replaceChildren(); area.hidden = !value.devices.length;
        if (area.hidden) return;
        const table = document.createElement('table'), head = table.createTHead().insertRow();
        for (const title of ['设备', '回复地址', 'Device 服务', '操作']) { const cell = document.createElement('th'); cell.scope = 'col'; cell.textContent = title; head.append(cell); }
        const body = table.createTBody();
        for (const device of value.devices) {
            const row = body.insertRow(); row.setAttribute('aria-selected', String(selected?.id === device.id));
            for (const text of [device.name || '未提供设备名称', device.peer_ip, device.service_url]) row.insertCell().textContent = text;
            const button = document.createElement('button'); button.type = 'button'; button.className = 'secondary'; button.textContent = '选择'; button.setAttribute('aria-label', `选择 ${device.name || device.peer_ip}`);
            button.onclick = () => { selected = device; clearProfiles(); node('onvif-manual').checked = false; node('onvif-manual-fields').hidden = true; node('onvif-selected').textContent = `${device.name || '设备'} · ${device.peer_ip}`; renderDevices(value); status('已选择设备，请填写所需凭据并读取 Profile。'); };
            row.insertCell().append(button);
        }
        area.append(table);
    }
    async function refreshJob(id, revision) {
        const requestID = ++pollRequest; clearTimeout(poll);
        try {
            const value = await request('/discoveries/' + id);
            if (revision !== generation || requestID !== pollRequest) return;
            job = value; renderDevices(value);
            if (value.state === 'running') { if (!operation) status(`正在搜索，已发现 ${value.devices.length} 台设备…`); poll = setTimeout(() => refreshJob(id, revision), 400); }
            else { node('onvif-search-button').disabled = false; node('onvif-stop').disabled = !operation; const label = {complete:'搜索完成',canceled:'已停止搜索',failed:'搜索失败'}[value.state] || value.state; if (!operation) status(`${label}，发现 ${value.devices.length} 台设备。${value.failure ? value.failure.error : ''}${value.response_limit_reached ? '已达到回复处理上限，可缩小搜索范围后重试。' : ''}`, value.state === 'failed'); }
        } catch (error) { if (revision === generation) { status(error.message, true); node('onvif-search-button').disabled = false; node('onvif-stop').disabled = true; } }
    }
    node('onvif-search').onsubmit = async event => {
        event.preventDefault(); clearTimeout(poll); const revision = ++generation; selected = undefined; clearProfiles(); node('onvif-devices').hidden = true; node('onvif-selected').textContent = '尚未选择设备';
        node('onvif-search-button').disabled = true;
        try { job = await request('/discoveries', {bind_ip:node('onvif-interface').value, seconds:Number(node('onvif-seconds').value)}); if (revision !== generation) return; node('onvif-stop').disabled = false; await refreshJob(job.id, revision); }
        catch (error) { status(error.message, true); node('onvif-search-button').disabled = false; }
    };
    node('onvif-stop').onclick = async () => {
        if (operation) { operation.abort(); return; }
        if (!job) return;
        try { await request('/discoveries/' + job.id + '/stop', {}); await refreshJob(job.id, generation); } catch (error) { status(error.message, true); }
    };
    async function querying(button, action) {
        if (operation) return;
        operation = new AbortController(); button.disabled = true; node('onvif-stop').disabled = false; node('onvif-stop').textContent = '取消查询';
        try { await action(operation.signal); } catch (error) { status(error.name === 'AbortError' ? 'ONVIF 查询已取消。' : error.message, error.name !== 'AbortError'); }
        finally { operation = undefined; button.disabled = false; node('onvif-stop').textContent = '停止搜索'; node('onvif-stop').disabled = job?.state !== 'running'; }
    }
    node('onvif-query').onsubmit = event => {
        event.preventDefault(); clearProfiles();
        querying(node('onvif-read-profiles'), async signal => {
            const revision = targetRevision;
            const manual = node('onvif-manual').checked;
            if (!manual && !selected) throw new Error('请先选择发现的设备，或填写手动服务地址。');
            const source = manual ? {service_url:node('onvif-service-url').value.trim()} : {discovery_id:job.id, device_id:selected.id};
            status('正在核对设备时间并读取媒体 Profile…');
            const result = await request('/profiles', {...source,bind_ip:node('onvif-interface').value,...credentials()}, signal);
            if (revision !== targetRevision) return;
            selection = result; const profiles = node('onvif-profile'); profiles.replaceChildren();
            for (const profile of result.profiles) { const option = document.createElement('option'); option.value = profile.token; option.textContent = [profile.name || profile.token,profile.video_codec,profile.width && profile.height ? `${profile.width}×${profile.height}` : '',profile.frame_rate ? `${profile.frame_rate} fps` : '',profile.audio_codec].filter(Boolean).join(' · '); profiles.append(option); }
            node('onvif-clock').textContent = result.clock_status === 'device_utc' ? `设备 ${result.peer_ip} · 已读取设备 UTC 时间，鉴权时间偏差约 ${result.clock_offset_seconds} 秒；未修改本机时间。` : '未取得可用于鉴权的设备时间，当前使用本机 UTC；鉴权失败时请核对设备时钟。';
            node('onvif-profiles').hidden = false; status(`已读取 ${result.profiles.length} 个 Profile。${result.dropped_profiles ? `另有 ${result.dropped_profiles} 个超出展示上限。` : ''}`);
        });
    };
    node('onvif-prefill').onclick = () => querying(node('onvif-prefill'), async signal => {
        if (!selection) throw new Error('请重新读取设备 Profile。');
        const revision = targetRevision, submittedCredentials = credentials();
        const result = await request('/stream', {selection_id:selection.selection_id,profile_token:node('onvif-profile').value,transport:node('onvif-transport').value,...submittedCredentials}, signal);
        if (revision !== targetRevision) return;
        const transport = node('rtsp-transport');
        if (result.transport === 'udp' && !transport) throw new Error('当前版本尚未提供 RTSP UDP 采样选项，请选择 TCP。');
        const address = new URL(result.url), copy = node('onvif-copy-credentials').checked && !result.different_host;
        if (copy) { address.username = submittedCredentials.username; address.password = submittedCredentials.password; }
        node('protocol').value = result.protocol; node('protocol').dispatchEvent(new Event('change'));
        if (transport) { transport.value = result.transport; transport.dispatchEvent(new Event('change')); }
        node('url').value = address.href; node('analyze-playback').checked = false; node('url').dispatchEvent(new Event('input'));
        status(`已回填 ${result.protocol.toUpperCase()} / ${result.transport.toUpperCase()} 地址，请核对后手动开始采样。${result.different_host ? '流主机与 ONVIF 服务不同，未复制凭据。' : ''}${result.removed_userinfo ? '设备返回地址中的内嵌凭据已移除。' : ''}`); node('url').focus();
    });
    node('onvif-manual').onchange = () => { node('onvif-manual-fields').hidden = !node('onvif-manual').checked; clearProfiles(); };
    node('onvif-service-url').oninput = clearProfiles;
    node('onvif-username').oninput = clearProfiles;
    node('onvif-password').oninput = clearProfiles;
    function mediaSelectionChanged() {
        targetRevision++;
        if (operation) { operation.abort(); status('媒体选择已更改，请重新获取播放地址。'); }
    }
    node('onvif-profile').onchange = mediaSelectionChanged;
    node('onvif-transport').onchange = mediaSelectionChanged;
    node('onvif-interface').onchange = () => { selected = undefined; clearProfiles(); node('onvif-selected').textContent = '网卡已切换，请重新搜索或使用手动地址。'; };
    request('/interfaces').then(values => { const field = node('onvif-interface'); field.replaceChildren(); for (const value of values) { const option = document.createElement('option'); option.value = value.ip; option.textContent = `${value.name} · ${value.ip}${value.loopback ? '（本机回环）' : ''}`; field.append(option); } if (!values.length) status('没有可用的 IPv4 网卡。', true); }).catch(error => status(error.message, true));
    window.addEventListener('pagehide', () => { clearTimeout(poll); operation?.abort(); if (job?.state === 'running') fetch(base + '/discoveries/' + job.id + '/stop', {method:'POST', keepalive:true}).catch(() => {}); });
})();
