// Copy-to-clipboard for terminal blocks and anything with [data-copy-target].
export default function initCopy() {
  document.addEventListener('click', ev => {
    const btn = ev.target.closest('[data-copy-target]')
    if (!btn) return

    const source = document.querySelector(btn.dataset.copyTarget)
    if (!source) return

    navigator.clipboard.writeText(source.innerText.trim()).then(() => {
      const icon = btn.querySelector('i') || btn
      const original = icon.className
      icon.className = 'bi bi-check-lg'
      setTimeout(() => { icon.className = original }, 1500)
    })
  })
}
