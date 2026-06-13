import * as bootstrap from 'bootstrap'
import { decodeHeifToCanvas } from './heif'

window.onload = (event) => {
//    if ((window.navigator.language == 'ru' || window.navigator.language == 'ru-RU') && document.documentElement.lang !== 'ru') {
//        location.href = location.href.replace(location.search, '').concat('?locale=ru');
//    }

    const modalZoom = document.getElementById('modalZoom');
    const zoom = new bootstrap.Modal(modalZoom, {});
    document.querySelectorAll('.img-zoom').forEach(el => {
        el.addEventListener('click', ev => {
          let b = modalZoom.querySelector('.modal-body');
          b.textContent = '';
          let i = document.createElement('img');
          i.src = ev.target.src;
          i.classList.add('img-fluid');
          b.appendChild(i);
          zoom.show();
        });
    });

    document.querySelectorAll('a[href^="http"], a[rel^="external"]').forEach(el => {
        el.target = '_blank';
        el.classList.add('external-link');
    });

    document.querySelectorAll('span[data-timestamp]').forEach(el => {
        const ts = el.dataset['timestamp'];
        let date = new Date(ts * 1000);
        el.textContent = date.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
    });

    // For .warning and .danger buttons, ask confirmation on action.
    document.querySelectorAll('.btn-danger, .btn-warning, .confirm').forEach(el => {
        // for input or button, find parent form and attach listener to its submit event
        if (el.nodeName === 'INPUT' || el.nodeName === 'BUTTON') {
            while (el.nodeName !== 'FORM') el = el.parentNode
            el.addEventListener('submit', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
        } else {
            el.addEventListener('click', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
        }
    });
};

// Progressive enhancement: view the *original* HEIF snapshot decoded in the
// browser (the page otherwise shows a server-rendered JPEG). See heif.js.
document.addEventListener('click', async (ev) => {
    const btn = ev.target.closest('[data-heif-view]');
    if (!btn) return;
    const wrap = btn.closest('[data-heif-original]');
    const url = wrap && wrap.dataset.heifOriginal;
    const canvas = wrap && wrap.querySelector('[data-heif-canvas]');
    const status = wrap && wrap.querySelector('[data-heif-status]');
    if (!url || !canvas) return;

    btn.disabled = true;
    if (status) status.textContent = 'Decoding…';
    try {
        const buffer = await (await fetch(url, { credentials: 'same-origin' })).arrayBuffer();
        const how = await decodeHeifToCanvas(buffer, canvas);
        canvas.classList.remove('d-none');
        btn.classList.add('d-none');
        if (status) status.textContent = `Original HEIF, decoded in your browser (${how}).`;
    } catch (e) {
        console.warn('HEIF decode failed', e);
        if (status) status.textContent = 'Your browser can’t decode this HEIF — showing the server preview.';
        btn.disabled = false;
    }
});
