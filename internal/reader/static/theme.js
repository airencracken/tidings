(() => {
  const root = document.documentElement;
  const select = document.querySelector('#theme');
  if (!select) return;
  const apply = value => {
    if (value === 'light' || value === 'dark') root.dataset.theme = value;
    else delete root.dataset.theme;
  };
  try { select.value = localStorage.getItem('tidings-theme') || 'system'; } catch {}
  apply(select.value);
  select.closest('label').hidden = false;
  select.addEventListener('change', () => {
    apply(select.value);
    try { localStorage.setItem('tidings-theme', select.value); } catch {}
  });
})();
