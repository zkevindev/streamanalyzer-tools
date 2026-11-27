'use strict';

(() => {
  const STORAGE_KEY = 'streamanalyzer.theme';

  function preferredTheme() {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved === 'dark' || saved === 'light') return saved;
    } catch { /* ignore storage errors */ }
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  function applyTheme(theme, persist = false) {
    const next = theme === 'dark' ? 'dark' : 'light';
    document.documentElement.dataset.theme = next;
    if (persist) {
      try { localStorage.setItem(STORAGE_KEY, next); } catch { /* ignore */ }
    }
    const btn = document.getElementById('theme-toggle');
    if (btn) {
      btn.setAttribute('aria-pressed', String(next === 'dark'));
      btn.textContent = next === 'dark' ? '浅色工作台' : '暗夜示波器';
    }
    window.dispatchEvent(new CustomEvent('workbench-theme-change', { detail: { theme: next } }));
    if (window.parent !== window) {
      window.parent.postMessage({ kind: 'workbench-theme', theme: next }, '*');
    }
  }

  applyTheme(preferredTheme(), false);

  document.addEventListener('DOMContentLoaded', () => {
    applyTheme(document.documentElement.dataset.theme || preferredTheme(), false);
    const btn = document.getElementById('theme-toggle');
    if (btn) {
      btn.addEventListener('click', () => {
        const current = document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';
        applyTheme(current === 'dark' ? 'light' : 'dark', true);
      });
    }
  });

  window.addEventListener('storage', (event) => {
    if (event.key === STORAGE_KEY && (event.newValue === 'dark' || event.newValue === 'light')) {
      applyTheme(event.newValue, false);
    }
  });

  if (window.parent !== window) {
    const sendLocation = () => {
      window.parent.postMessage({ kind: 'workbench-location', path: window.location.pathname }, '*');
    };
    window.addEventListener('message', (event) => {
      if (event.source !== window.parent) return;
      if (event.data?.kind === 'workbench-location-query') sendLocation();
      if (event.data?.kind === 'workbench-theme' && (event.data.theme === 'dark' || event.data.theme === 'light')) {
        applyTheme(event.data.theme, true);
      }
    });
    window.addEventListener('pageshow', sendLocation);
    window.addEventListener('popstate', sendLocation);
    window.addEventListener('workbench-location-change', sendLocation);
  }
})();
