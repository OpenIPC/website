function $(n) {
    return document.querySelector(n);
}

function $$(n) {
    return document.querySelectorAll(n);
}

// For .warning and .danger buttons, ask confirmation on action.
$$('.btn-danger, .btn-warning, .confirm').forEach(el => {
    // for input, find its parent form and attach listener to it submit event
    if (el.nodeName === "INPUT") {
        while (el.nodeName !== "FORM") el = el.parentNode
        el.addEventListener('submit', ev => (!confirm("Are you sure?")) ? ev.preventDefault() : null)
    } else {
        el.addEventListener('click', ev => (!confirm("Are you sure?")) ? ev.preventDefault() : null)
    }
});

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
