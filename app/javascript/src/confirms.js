// Ask for confirmation before destructive actions (.btn-danger, .btn-warning, .confirm).
// `root` scopes the walk. It matters because this one ADDS A LISTENER and has
// no bound-marker: running it twice over the same form means two confirm()
// dialogues for one click. Since #261 a lazy turbo-frame can finish loading
// long after turbo:load, and the admin delete buttons on a snapshot page sit
// OUTSIDE that frame -- so a document-wide re-run would rebind them.
export default function initConfirms(root = document) {
  root.querySelectorAll('.btn-danger, .btn-warning, .confirm').forEach(el => {
    // for input or button, find parent form and attach listener to its submit event
    if (el.nodeName === 'INPUT' || el.nodeName === 'BUTTON') {
      while (el && el.nodeName !== 'FORM') el = el.parentNode
      if (el) el.addEventListener('submit', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
    } else {
      el.addEventListener('click', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
    }
  })
}
