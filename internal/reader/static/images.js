document.addEventListener('error', event => {
  if (event.target instanceof HTMLImageElement && event.target.matches('[data-feed-image]')) {
    const frame = event.target.closest('[data-image-frame]');
    (frame || event.target).remove();
  }
}, true);
