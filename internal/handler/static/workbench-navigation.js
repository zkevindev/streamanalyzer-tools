'use strict';

if (window.parent !== window) {
  const sendLocation = () => {
    window.parent.postMessage({ kind: 'workbench-location', path: window.location.pathname }, '*');
  };
  window.addEventListener('message', (event) => {
    if (event.source === window.parent && event.data?.kind === 'workbench-location-query') sendLocation();
  });
  window.addEventListener('pageshow', sendLocation);
}
