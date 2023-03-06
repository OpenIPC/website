function $(n) {
    return document.querySelector(n);
}

function $$(n) {
    return document.querySelectorAll(n);
}

window.onload = (event) => {
    $$('a[href^="http"], a[rel^="external"]').forEach(el => {
        el.target = '_blank';
        el.classList.add('external-link');
    });

    if (document.documentElement.lang !== 'ru') {
        $$('.lang-ru').forEach(el => el.classList.add('d-none'));
    }

    $$('span[data-timestamp]').forEach(el => {
        const ts = el.dataset['timestamp'];
        let date = new Date(ts * 1000);
        el.textContent = date.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
    });

    // For .warning and .danger buttons, ask confirmation on action.
    $$('.btn-danger, .btn-warning, .confirm').forEach(el => {
        // for input or button, find parent form and attach listener to its submit event
        if (el.nodeName === 'INPUT' || el.nodeName === 'BUTTON') {
            while (el.nodeName !== "FORM") el = el.parentNode
            el.addEventListener('submit', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
        } else {
            el.addEventListener('click', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
        }
    });
};
