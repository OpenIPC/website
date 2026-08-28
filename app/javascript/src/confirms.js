// Ask for confirmation before destructive actions (.btn-danger, .btn-warning, .confirm).
export default function initConfirms() {
  document.querySelectorAll('.btn-danger, .btn-warning, .confirm').forEach(el => {
    // for input or button, find parent form and attach listener to its submit event
    if (el.nodeName === 'INPUT' || el.nodeName === 'BUTTON') {
      while (el && el.nodeName !== 'FORM') el = el.parentNode
      if (el) el.addEventListener('submit', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
    } else {
      el.addEventListener('click', ev => (!confirm('Are you sure?')) ? ev.preventDefault() : null)
    }
  })
}
