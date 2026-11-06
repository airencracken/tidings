document.addEventListener('error', event => {
  if (event.target instanceof HTMLImageElement && event.target.matches('[data-feed-image]')) {
    event.target.closest('[data-image-frame]')?.remove();
  }
}, true);
