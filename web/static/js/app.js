if ('serviceWorker' in navigator) {
  window.addEventListener('load', function () {
    navigator.serviceWorker.register('/service-worker.js').catch(function (e) {
      console.warn('SW registration failed:', e);
    });
  });
}
