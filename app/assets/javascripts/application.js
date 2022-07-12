function $(n) {
    return document.querySelector(n);
}

function $$(n) {
    return document.querySelectorAll(n);
}

window.onload = (event) => {
    $$('a[href^="http"]').forEach(el => {
        el.target = "_blank";
        el.classList.add('external-link');
    });
    $$('a[rel^="external"]').forEach(el => {
        el.target = "_blank";
        el.classList.add('external-link');
    });
};
