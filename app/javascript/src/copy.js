// Copy-to-clipboard for terminal blocks and anything with [data-copy-target].
export default function initCopy() {
  document.addEventListener('click', ev => {
    const btn = ev.target.closest('[data-copy-target]')
    if (!btn) return

    const source = document.querySelector(btn.dataset.copyTarget)
    if (!source) return

    // textContent, not innerText. innerText is the *rendered* text, so with the
    // terminal block now soft-wrapping a long URL there is no guarantee an
    // engine will not fold those visual breaks into the string. textContent is
    // the markup's own text: exactly the command, with only the newlines that
    // were actually written.
    navigator.clipboard.writeText(source.textContent.trim()).then(() => {
      const icon = btn.querySelector('i') || btn
      const original = icon.className
      icon.className = 'bi bi-check-lg'
      setTimeout(() => { icon.className = original }, 1500)
    })
  })
}
