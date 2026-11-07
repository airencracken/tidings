(() => {
  const root = document.documentElement;
  const apply = value => {
    if (value === 'light' || value === 'dark') root.dataset.theme = value;
    else delete root.dataset.theme;
  };
  let saved = 'system';
  try { saved = localStorage.getItem('tidings-theme') || 'system'; } catch {}
  apply(saved);
  // Run in the head before the page can paint. Bind the picker once it exists.
  document.addEventListener('DOMContentLoaded', () => {
    const select = document.querySelector('#theme');
    if (!select) return;
    select.value = root.dataset.theme || 'system';
    select.closest('label').hidden = false;
    select.addEventListener('change', () => {
      apply(select.value);
      try { localStorage.setItem('tidings-theme', select.value); } catch {}
    });
  }, { once: true });
})();
