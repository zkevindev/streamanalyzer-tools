'use strict';

(() => {
    const node = (id) => document.getElementById(id);
    const linkByPath = new Map([
        ['/diagnostics', node('capture-tool-link')],
        ['/diagnostics/onvif', node('onvif-tool-link')],
        ['/diagnostics/review', node('review-tool-link')],
    ]);
    const modeLinks = [...linkByPath.values(), node('capture-mode-link'), node('import-mode-link')];
    const workbenchTab = document.querySelector('.workbench-tab[href="/diagnostics"]');
    if (!workbenchTab || modeLinks.some((link) => !link)) return;

    const supportedRealtime = new Set(['rtmp', 'http-flv', 'hls']);
    const protocol = node('protocol');
    const address = node('url');
    const realtime = node('realtime-prefill');

    function updateView() {
        const path = location.pathname;
        const onvif = path === '/diagnostics/onvif';
        const review = path === '/diagnostics/review';
        node('capture-workbench').hidden = onvif;
        node('onvif-discovery').hidden = !onvif;
        node('capture-panel').hidden = review;
        node('capture-duration-hint').hidden = review;
        node('evidence-import-panel').hidden = !review;
        node('stream-modes').hidden = review;
        node('review-modes').hidden = !review;
        node('diagnostics-title').textContent = review ? '离线复盘' : '流媒体排障';
        for (const [href, link] of linkByPath) {
            if (href === path) link.setAttribute('aria-current', 'page');
            else link.removeAttribute('aria-current');
        }
        workbenchTab.setAttribute('aria-current', onvif || review ? 'true' : 'page');
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
    window.addEventListener('open-diagnostic-tool', (event) => switchTo(event.detail));
    window.addEventListener('popstate', updateView);
    window.addEventListener('message', (event) => {
        if (event.source !== window.parent || event.data?.kind !== 'workbench-return-to-capture') return;
        switchTo('/diagnostics');
        node('capture-tool-link').focus();
    });

    function updateRealtimeAction() {
        realtime.hidden = !supportedRealtime.has(protocol.value) || !address.value.trim() || !address.validity.valid;
    }
    protocol.addEventListener('change', updateRealtimeAction);
    address.addEventListener('input', updateRealtimeAction);
    realtime.addEventListener('click', (event) => {
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
        const selectedProtocol = protocol.value;
        if (realtime.hidden) return;
        try {
            sessionStorage.setItem('streamanalyzer.realtime-input', JSON.stringify({
                url: address.value.trim(), protocol: selectedProtocol, expires: Date.now() + 30_000,
            }));
        } catch {
            // Keep the regular realtime link available if the browser blocks storage.
        }
    });
    updateRealtimeAction();
    updateView();
})();
