'use strict';
(() => {
    const node = id => document.getElementById(id), base = '/api/diagnostics/onvif';
    const dialog = node('onvif-discovery'), opener = node('open-onvif-discovery');
    if (!dialog || !opener) return;
    const nativeDialog = typeof dialog.showModal === 'function';
    const isOpen = () => dialog.hasAttribute('open');
    const searchButton = node('onvif-search-button'), stopButton = node('onvif-stop');
    const interfaceField = node('onvif-interface'), manualField = node('onvif-manual');
    const serviceURL = node('onvif-service-url'), prefillStatus = node('onvif-prefill-status');
    const username = node('onvif-username'), password = node('onvif-password');
    let job, selected, selection, poll, operation, interfacesLoaded, loadingInterfaces;
    let generation = 0, targetRevision = 0, pollRequest = 0;

    function status(text, error = false) {
        const element = node('onvif-status');
        element.textContent = text;
        element.dataset.error = String(error);
    }
    async function request(path, body, signal) {
        const response = await fetch(base + path, body === undefined ? {signal} : {
            method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body), signal,
        });
        const value = await response.json();
        if (!response.ok) throw new Error(`${value.error || 'ONVIF 请求失败'}${value.stage ? `（${value.stage}）` : ''}`);
        return value;
    }
    function clearProfiles() {
        targetRevision++;
        operation?.abort();
        selection = undefined;
        node('onvif-profiles').hidden = true;
        node('onvif-copy-credentials').checked = false;
    }
    function credentials() { return {username: username.value, password: password.value}; }
    function updateDevice() {
        node('onvif-selected').textContent = selected ? `${selected.name || '设备'} · ${selected.peer_ip}` : '尚未选择设备';
        node('onvif-selected').hidden = manualField.checked;
        node('onvif-manual-fields').hidden = !manualField.checked;
        node('onvif-credentials').hidden = !manualField.checked && !selected;
        serviceURL.disabled = !manualField.checked;
        serviceURL.required = manualField.checked;
    }
    function renderDevices(devices) {
        const area = node('onvif-devices');
        const focusedDevice = area.contains(document.activeElement) ? document.activeElement.dataset.device : undefined;
        area.replaceChildren();
        area.hidden = !devices.length;
        if (area.hidden) return;
        const table = document.createElement('table'), head = table.createTHead().insertRow();
        for (const title of ['设备', '回复地址', 'Device 服务', '操作']) {
            const cell = document.createElement('th');
            cell.scope = 'col';
            cell.textContent = title;
            head.append(cell);
        }
        const body = table.createTBody();
        let focusTarget;
        for (const device of devices) {
            const row = body.insertRow();
            row.setAttribute('aria-selected', String(!manualField.checked && selected?.id === device.id));
            for (const text of [device.name || '未提供设备名称', device.peer_ip, device.service_url]) {
                row.insertCell().textContent = text;
            }
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'secondary';
            button.textContent = '选择';
            button.dataset.device = device.id;
            button.setAttribute('aria-label', `选择 ${device.name || device.peer_ip}`);
            if (focusedDevice === device.id) focusTarget = button;
            button.onclick = () => {
                selected = device;
                clearProfiles();
                manualField.checked = false;
                updateDevice();
                renderDevices(job.devices);
                username.focus();
            };
            row.insertCell().append(button);
        }
        area.append(table);
        focusTarget?.focus({preventScroll: true});
    }
    async function refreshJob(id, revision) {
        const requestID = ++pollRequest;
        clearTimeout(poll);
        try {
            const value = await request('/discoveries/' + id);
            if (revision !== generation || requestID !== pollRequest) return;
            job = value;
            renderDevices(value.devices);
            if (value.state === 'running') {
                if (!operation) status(`正在搜索，已发现 ${value.devices.length} 台设备…`);
                poll = setTimeout(() => refreshJob(id, revision), 400);
                return;
            }
            searchButton.disabled = false;
            stopButton.disabled = !operation;
            const label = {complete: '搜索完成', canceled: '已停止搜索', failed: '搜索失败'}[value.state] || value.state;
            if (!operation) status(`${label}，发现 ${value.devices.length} 台设备。${value.failure?.error || ''}${value.response_limit_reached ? '已达到回复处理上限。' : ''}`, value.state === 'failed');
        } catch (error) {
            if (revision !== generation || requestID !== pollRequest) return;
            status(error.message, true);
            searchButton.disabled = false;
            stopButton.disabled = !operation;
        }
    }
    function stopInBackground(id) {
        fetch(base + '/discoveries/' + id + '/stop', {method: 'POST', keepalive: true}).catch(() => {});
    }
    node('onvif-search').onsubmit = async event => {
        event.preventDefault();
        if (searchButton.disabled) return;
        clearTimeout(poll);
        const revision = ++generation;
        selected = undefined;
        job = undefined;
        clearProfiles();
        renderDevices([]);
        updateDevice();
        searchButton.disabled = true;
        status('正在启动搜索…');
        try {
            const value = await request('/discoveries', {bind_ip: interfaceField.value, seconds: Number(node('onvif-seconds').value)});
            if (revision !== generation) {
                if (value.state === 'running') stopInBackground(value.id);
                return;
            }
            job = value;
            stopButton.disabled = false;
            await refreshJob(job.id, revision);
        } catch (error) {
            if (revision !== generation) return;
            status(error.message, true);
            searchButton.disabled = false;
        }
    };
    stopButton.onclick = async () => {
        if (operation) { operation.abort(); return; }
        if (!job) return;
        const {id} = job, revision = generation;
        try {
            await request('/discoveries/' + id + '/stop', {});
            if (revision === generation) await refreshJob(id, revision);
        } catch (error) {
            if (revision === generation) status(error.message, true);
        }
    };
    async function querying(button, action) {
        if (operation) return;
        operation = new AbortController();
        button.disabled = true;
        stopButton.disabled = false;
        stopButton.textContent = '取消查询';
        try {
            await action(operation.signal);
        } catch (error) {
            if (isOpen()) status(error.name === 'AbortError' ? 'ONVIF 查询已取消。' : error.message, error.name !== 'AbortError');
        } finally {
            operation = undefined;
            button.disabled = false;
            stopButton.textContent = '停止搜索';
            stopButton.disabled = job?.state !== 'running';
        }
    }
    node('onvif-query').onsubmit = event => {
        event.preventDefault();
        if (!manualField.checked && !selected) return;
        clearProfiles();
        querying(node('onvif-read-profiles'), async signal => {
            const revision = targetRevision;
            const source = manualField.checked ? {service_url: serviceURL.value.trim()} : {discovery_id: job.id, device_id: selected.id};
            status('正在核对设备时间并读取媒体 Profile…');
            const result = await request('/profiles', {...source, bind_ip: interfaceField.value, ...credentials()}, signal);
            if (!isOpen() || signal.aborted || revision !== targetRevision) return;
            selection = result;
            const profiles = node('onvif-profile');
            profiles.replaceChildren();
            for (const profile of result.profiles) {
                const option = document.createElement('option');
                option.value = profile.token;
                const size = profile.width && profile.height ? `${profile.width}×${profile.height}` : '';
                const fps = profile.frame_rate ? `${profile.frame_rate} fps` : '';
                option.textContent = [profile.name || profile.token, profile.video_codec, size, fps, profile.audio_codec].filter(Boolean).join(' · ');
                profiles.append(option);
            }
            node('onvif-clock').textContent = result.clock_status === 'device_utc'
                ? `设备 ${result.peer_ip} · ONVIF 鉴权时间已根据设备 UTC 校正，偏差约 ${result.clock_offset_seconds} 秒。`
                : '当前使用本机 UTC 鉴权，失败时请核对设备时钟。';
            node('onvif-profiles').hidden = false;
            status(`已读取 ${result.profiles.length} 个 Profile。${result.dropped_profiles ? `另有 ${result.dropped_profiles} 个超出展示上限。` : ''}`);
            profiles.focus();
        });
    };
    node('onvif-prefill').onclick = () => querying(node('onvif-prefill'), async signal => {
        if (!selection) throw new Error('请重新读取设备 Profile。');
        const revision = targetRevision, submittedCredentials = credentials();
        status('正在获取 RTSP 播放地址…');
        const result = await request('/stream', {
            selection_id: selection.selection_id, profile_token: node('onvif-profile').value,
            transport: node('onvif-transport').value, ...submittedCredentials,
        }, signal);
        if (!isOpen() || signal.aborted || revision !== targetRevision) return;
        const transport = node('rtsp-transport');
        if (result.transport === 'udp' && !transport) throw new Error('当前版本尚未提供 RTSP UDP 采样选项，请选择 TCP。');
        const address = new URL(result.url);
        if (node('onvif-copy-credentials').checked && !result.different_host) {
            address.username = submittedCredentials.username;
            address.password = submittedCredentials.password;
        }
        node('protocol').value = result.protocol;
        node('protocol').dispatchEvent(new Event('change'));
        if (transport) {
            transport.value = result.transport;
            transport.dispatchEvent(new Event('change'));
        }
        node('url').value = address.href;
        node('analyze-playback').checked = false;
        node('url').dispatchEvent(new Event('input'));
        closeDiscovery();
        prefillStatus.textContent = `已回填 ${result.protocol.toUpperCase()} / ${result.transport.toUpperCase()} 地址，请核对后开始采样。${result.different_host ? '流主机与设备服务不同，未复制凭据。' : ''}${result.removed_userinfo ? '已移除设备返回的内嵌凭据。' : ''}`;
        node('url').focus();
    });
    function cancelSearch() {
        generation++;
        clearTimeout(poll);
        if (job?.state === 'running') {
            stopInBackground(job.id);
            job = {...job, state: 'canceled'};
        }
        searchButton.disabled = !interfaceField.value;
        stopButton.disabled = !operation;
    }
    function endSession() {
        clearProfiles();
        cancelSearch();
        username.value = '';
        password.value = '';
        serviceURL.value = '';
        manualField.checked = false;
        updateDevice();
        if (job) renderDevices(job.devices);
        status(job?.devices.length ? '选择已发现的设备，或重新搜索。' : '选择网卡搜索设备，或手动填写服务地址。');
    }
    async function loadInterfaces() {
        try {
            const values = await request('/interfaces');
            interfaceField.replaceChildren();
            for (const value of values) {
                const option = document.createElement('option');
                option.value = value.ip;
                option.textContent = `${value.name} · ${value.ip}${value.loopback ? '（本机回环）' : ''}`;
                interfaceField.append(option);
            }
            interfacesLoaded = true;
            searchButton.disabled = !values.length;
            if (!values.length && isOpen()) status('没有可用的 IPv4 网卡。', true);
        } catch (error) {
            if (isOpen()) status(error.message, true);
        }
    }
    manualField.onchange = () => {
        clearProfiles();
        updateDevice();
        if (job) renderDevices(job.devices);
        if (manualField.checked) serviceURL.focus();
    };
    serviceURL.oninput = clearProfiles;
    username.oninput = clearProfiles;
    password.oninput = clearProfiles;
    function mediaSelectionChanged() {
        targetRevision++;
        if (operation) {
            operation.abort();
            status('媒体选择已更改，请重新获取播放地址。');
        }
    }
    node('onvif-profile').onchange = mediaSelectionChanged;
    node('onvif-transport').onchange = mediaSelectionChanged;
    interfaceField.onchange = () => {
        clearProfiles();
        cancelSearch();
        job = undefined;
        selected = undefined;
        renderDevices([]);
        updateDevice();
        status('网卡已切换，请重新搜索或填写服务地址。');
    };
    function closeDiscovery() {
        endSession();
        if (nativeDialog) dialog.close();
        else {
            dialog.removeAttribute('open');
            node('onvif-backdrop').hidden = true;
        }
        opener.focus();
    }
    opener.onclick = () => {
        if (nativeDialog) dialog.showModal();
        else {
            dialog.classList.add('onvif-fallback');
            dialog.setAttribute('role', 'dialog');
            dialog.setAttribute('aria-modal', 'true');
            dialog.setAttribute('open', '');
            node('onvif-backdrop').hidden = false;
            interfaceField.focus();
        }
        if (!interfacesLoaded) loadingInterfaces ??= loadInterfaces().finally(() => { loadingInterfaces = undefined; });
        else if (!interfaceField.value) status('没有可用的 IPv4 网卡。', true);
    };
    node('close-onvif-discovery').onclick = closeDiscovery;
    dialog.addEventListener('cancel', event => {
        event.preventDefault();
        closeDiscovery();
    });
    if (!nativeDialog) document.addEventListener('keydown', event => {
        if (!isOpen()) return;
        if (event.key === 'Escape') {
            event.preventDefault();
            closeDiscovery();
            return;
        }
        if (event.key !== 'Tab') return;
        const controls = [...dialog.querySelectorAll('button, input, select, [tabindex="0"]')]
            .filter(control => !control.matches(':disabled, [hidden], [hidden] *'));
        const next = event.shiftKey ? controls[controls.length - 1] : controls[0];
        const boundary = event.shiftKey ? controls[0] : controls[controls.length - 1];
        if (!dialog.contains(document.activeElement) || document.activeElement === boundary) {
            event.preventDefault();
            next.focus();
        }
    });
    window.addEventListener('pagehide', endSession);
    node('url').addEventListener('input', () => { prefillStatus.textContent = ''; });
    node('protocol').addEventListener('change', () => { prefillStatus.textContent = ''; });
    updateDevice();
    opener.hidden = false;
})();
