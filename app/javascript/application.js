import * as bootstrap from 'bootstrap'

window.onload = (event) => {
//    if ((window.navigator.language == 'ru' || window.navigator.language == 'ru-RU') && document.documentElement.lang !== 'ru') {
//        location.href = location.href.replace(location.search, '').concat('?locale=ru');
//    }

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
            while (el.nodeName !== "FORM") el = el.parentNode
            el.addEventListener('submit', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
        } else {
            el.addEventListener('click', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
        }
    });
};
