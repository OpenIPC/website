// The share page's own flight recorder: what it did and when, for a guest to
// copy and send when something does not work. Kept in memory, bounded, and
// never holding the link's secret -- only the share id, which is in the host
// name anyway.
const START = performance.now();
const LINES = [];
const MAX = 300;

export function trace(what, detail) {
  const t = ((performance.now() - START) / 1000).toFixed(2).padStart(7);
  let line = `${t}s  ${what}`;
  if (detail !== undefined) line += '  ' + (typeof detail === 'string' ? detail : JSON.stringify(detail));
  LINES.push(line.slice(0, 400));
  if (LINES.length > MAX) LINES.splice(0, LINES.length - MAX);
}

export function report(extra = {}) {
  const head = {
    page: location.host,
    at: new Date().toISOString(),
    browser: navigator.userAgent,
    online: navigator.onLine,
    serviceWorker: 'serviceWorker' in navigator
      ? (navigator.serviceWorker.controller ? 'controlling' : 'not controlling') : 'unsupported',
    ...extra,
  };
  return ['OpenIPC share diagnostics', ...Object.entries(head).map(([k, v]) => `${k}: ${v}`), '', ...LINES].join('\n');
}

// A "Show details" control with the report and a Copy button, appended to
// `parent`. The report is taken when it is opened, so it is current.
export function detailsControl(parent, extra) {
  const wrap = document.createElement('div');
  wrap.className = 'diag';
  const toggle = Object.assign(document.createElement('button'), { type: 'button', textContent: 'Show details' });
  const box = document.createElement('div');
  box.hidden = true;
  const pre = document.createElement('pre');
  const copy = Object.assign(document.createElement('button'), { type: 'button', textContent: 'Copy diagnostics' });
  const hint = Object.assign(document.createElement('p'), {
    textContent: 'Send this to whoever shared the camera with you. It contains no password or link secret.',
  });
  toggle.onclick = () => {
    box.hidden = !box.hidden;
    toggle.textContent = box.hidden ? 'Show details' : 'Hide details';
    if (!box.hidden) pre.textContent = report(typeof extra === 'function' ? extra() : extra);
  };
  copy.onclick = async () => {
    const text = report(typeof extra === 'function' ? extra() : extra);
    pre.textContent = text;
    try {
      await navigator.clipboard.writeText(text);
      copy.textContent = 'Copied';
    } catch (e) {
      const range = document.createRange();
      range.selectNodeContents(pre);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
      copy.textContent = 'Selected: press Ctrl/Cmd+C';
    }
  };
  box.append(hint, pre, copy);
  wrap.append(toggle, box);
  parent.append(wrap);
  return wrap;
}
