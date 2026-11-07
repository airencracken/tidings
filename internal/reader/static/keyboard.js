(() => {
  // This script is included only for readers who explicitly enable it.
  const stories = Array.from(document.querySelectorAll('[data-story]'));
  if (!stories.length) return;
  let selected = -1;
  const select = index => {
    if (!stories[index]) return;
    stories.forEach(story => story.classList.remove('keyboard-selected'));
    selected = index;
    const story = stories[index];
    story.classList.add('keyboard-selected');
    story.focus({ preventScroll: true });
    story.scrollIntoView({ block: 'nearest', behavior: 'instant' });
  };
  document.addEventListener('focusin', event => {
    const index = stories.findIndex(story => story.contains(event.target));
    if (index >= 0) selected = index;
  });
  document.addEventListener('keydown', event => {
    if (event.defaultPrevented || event.repeat || event.isComposing || event.ctrlKey || event.altKey || event.metaKey) return;
    const target = event.target;
    if (target.closest('input,textarea,select,button,a,[contenteditable]:not([contenteditable="false"]),[role="textbox"],dialog')) return;
    let action;
    if (event.key === 'j') action = () => select(Math.min(selected + 1, stories.length - 1));
    if (event.key === 'k') action = () => select(Math.max(selected - 1, 0));
    const story = stories[Math.max(selected, 0)];
    if (event.key === 'Enter') {
      const link = story.querySelector('[data-story-open]');
      if (link) action = () => link.click();
    }
    if (event.key === 'm' || event.key === 's') {
      const field = event.key === 'm' ? 'read' : 'saved';
      const form = Array.from(story.querySelectorAll('form')).find(form => form.elements.field?.value === field);
      if (form) action = () => form.requestSubmit();
    }
    if (event.key === '/') {
      const search = document.getElementById('search');
      if (search) action = () => search.focus();
    }
    if (event.key === '?') action = () => { document.getElementById('keyboard-help')?.showModal(); };
    if (action) { event.preventDefault(); action(); }
  });
})();
