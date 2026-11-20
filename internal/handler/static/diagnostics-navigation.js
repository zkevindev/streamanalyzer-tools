'use strict';

(() => {
    const node = (id) => document.getElementById(id);
    const linkByPath = new Map([
        ['/diagnostics', node('capture-tool-link')],
        ['/diagnostics/onvif', node('onvif-tool-link')],
        ['/diagnostics/review', node('review-tool-link')],
    ]);
    const modeLinks = [...linkByPath.values(), node('import-mode-link')].filter(Boolean);
    const workbenchTab = document.querySelector('.workbench-tab[href="/diagnostics"]');
    if (!workbenchTab || !linkByPath.get('/diagnostics')) return;

    const protocol = node('protocol');
    const address = node('url');
    const sampleForm = node('sample');
    const workspace = document.querySelector('.workspace');
    const hlsModeSwitch = node('hls-mode-switch');
    const hlsDeliveryPanel = node('hls-delivery-panel');
    const hlsResults = node('hls-results');
    const hlsURL = node('hls-url');
    const captureHeading = node('capture-heading');
    const durationHint = node('capture-duration-hint');
    let hlsMode = 'capture';

    function syncHLSModeUI() {
        const isCapturePage = location.pathname === '/diagnostics';
        const isHLS = protocol.value === 'hls';
        if (!isHLS) hlsMode = 'capture';
        const delivery = isCapturePage && isHLS && hlsMode === 'delivery';

        if (hlsModeSwitch) {
            hlsModeSwitch.hidden = !(isCapturePage && isHLS);
            for (const btn of hlsModeSwitch.querySelectorAll('button[data-hls-mode]')) {
                btn.setAttribute('aria-selected', String(btn.dataset.hlsMode === (delivery ? 'delivery' : 'capture')));
            }
        }
        if (sampleForm) sampleForm.hidden = delivery;
        if (hlsDeliveryPanel) hlsDeliveryPanel.hidden = !delivery;
        if (workspace) workspace.hidden = delivery;
        if (hlsResults) {
            const hasReport = Boolean(node('hls-report')?.childElementCount);
            hlsResults.hidden = !delivery || !hasReport;
        }
        if (captureHeading) captureHeading.textContent = delivery ? 'HLS 交付分析' : '流媒体采样';
        if (durationHint) {
            durationHint.hidden = location.pathname === '/diagnostics/review' || delivery;
        }
    }

    function setHLSMode(nextMode) {
        if (nextMode !== 'capture' && nextMode !== 'delivery') return;
        if (nextMode === 'delivery' && protocol.value !== 'hls') {
            protocol.value = 'hls';
            protocol.dispatchEvent(new Event('change'));
        }
        if (nextMode === 'delivery' && hlsURL && !hlsURL.value.trim() && address.value.trim()) {
            hlsURL.value = address.value.trim();
        } else if (nextMode === 'capture' && hlsURL && hlsURL.value.trim() && !address.value.trim()) {
            address.value = hlsURL.value.trim();
        }
        hlsMode = nextMode;
        syncHLSModeUI();
    }

    function updateView() {
        const path = location.pathname;
        const onvif = path === '/diagnostics/onvif';
        const review = path === '/diagnostics/review';
        node('capture-workbench').hidden = onvif;
        node('onvif-discovery').hidden = !onvif;
        node('capture-panel').hidden = review;
        node('evidence-import-panel').hidden = !review;
        const reviewModes = node('review-modes');
        if (reviewModes) reviewModes.hidden = !review;
        node('diagnostics-title').textContent = review ? '离线复盘' : '流媒体排障';
        for (const [href, link] of linkByPath) {
            if (!link) continue;
            if (href === path) link.setAttribute('aria-current', 'page');
            else link.removeAttribute('aria-current');
        }
        workbenchTab.setAttribute('aria-current', onvif || review ? 'true' : 'page');
        syncHLSModeUI();
        window.dispatchEvent(new Event('diagnostic-tool-change'));
    }

    function switchTo(path) {
        if (!linkByPath.has(path) || path === location.pathname) return;
        history.pushState(null, '', path + location.search + location.hash);
        updateView();
        window.dispatchEvent(new Event('workbench-location-change'));
    }

    for (const link of modeLinks) {
        link.addEventListener('click', (event) => {
            if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
            event.preventDefault();
            switchTo(link.pathname);
        });
    }
    if (hlsModeSwitch) {
        hlsModeSwitch.addEventListener('click', (event) => {
            const btn = event.target.closest('button[data-hls-mode]');
            if (!btn) return;
            setHLSMode(btn.dataset.hlsMode);
        });
    }
    window.addEventListener('open-diagnostic-tool', (event) => switchTo(event.detail));
    window.addEventListener('popstate', updateView);
    window.addEventListener('message', (event) => {
        if (event.source !== window.parent || event.data?.kind !== 'workbench-return-to-capture') return;
        switchTo('/diagnostics');
        node('capture-tool-link').focus();
    });

    protocol.addEventListener('change', syncHLSModeUI);

    // Honor query parameters such as ?protocol=hls&hls_mode=delivery&task=...
    const params = new URLSearchParams(location.search);
    if (params.get('protocol') === 'hls' || params.get('hls_mode') === 'delivery' || params.get('task')) {
        if (protocol.value !== 'hls') {
            protocol.value = 'hls';
            protocol.dispatchEvent(new Event('change'));
        }
        if (params.get('hls_mode') === 'delivery' || params.get('task')) {
            hlsMode = 'delivery';
        }
    }

    updateView();
})();
