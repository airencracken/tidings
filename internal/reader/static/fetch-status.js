(() => {
  const panel = document.querySelector('[data-fetch-status]');
  if (!panel) return;
  const initialCheck = Number(panel.dataset.checked || 0);
  let timer, pending = false, stopped = false;
  const message = panel.querySelector('[data-fetch-message]');
  const detail = panel.querySelector('[data-fetch-detail]');
  const next = panel.querySelector('[data-fetch-next]');
  const update = panel.querySelector('[data-fetch-update]');
  const active = () => ['queued', 'fetching', 'waiting'].includes(panel.dataset.state);
  async function poll() {
    clearTimeout(timer);
    if (stopped || pending) return;
    if (document.hidden) { timer = setTimeout(poll, 30000); return; }
    pending = true;
    try {
      const response = await fetch('/api/v1/fetch-status', {
        credentials: 'same-origin', cache: 'no-store',
        headers: { Accept: 'application/json' }, signal: AbortSignal.timeout(10000),
      });
      if (response.redirected) { stopped = true; return; }
      if (!response.ok) throw new Error('status unavailable');
      const status = await response.json();
      if (typeof status.message !== 'string' || typeof status.state !== 'string') throw new Error('invalid status');
      panel.dataset.state = status.state;
      message.textContent = status.message;
      detail.textContent = status.detail || '';
      next.textContent = status.next_check || '';
      update.hidden = active() || !(status.checked > initialCheck);
    } catch {
      detail.textContent = 'Could not update fetch status. Trying again shortly.';
    } finally {
      pending = false;
      if (!stopped) timer = setTimeout(poll, active() ? 2000 : 30000);
    }
  }
  document.addEventListener('visibilitychange', () => { if (!document.hidden) poll(); });
  window.addEventListener('pagehide', () => { stopped = true; clearTimeout(timer); });
  poll();
})();
