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
    const durationHint = node('capture-duration-hint');

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
        if (durationHint) durationHint.hidden = review;
        node('diagnostics-title').textContent = review ? '离线复盘' : '流媒体排障';
        node('review-description').hidden = !review;
        document.title = (onvif ? 'ONVIF 设备探测' : review ? '离线复盘' : '流媒体排障') + ' · Stream Analyzer';
        if (node('report').hidden && node('stop').hidden) {
            node('result-heading').textContent = review ? '复盘报告' : '采样结果';
        }
        for (const [href, link] of linkByPath) {
            if (!link) continue;
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

    const params = new URLSearchParams(location.search);
    if (params.get('protocol') === 'hls' && protocol && protocol.value !== 'hls') {
        protocol.value = 'hls';
        protocol.dispatchEvent(new Event('change'));
    }

    updateView();
})();
