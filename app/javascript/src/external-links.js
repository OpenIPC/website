// Open external links in a new tab and mark them visually.
export default function initExternalLinks() {
  document.querySelectorAll('a[href^="http"], a[rel^="external"]').forEach(el => {
    el.target = '_blank'
    el.rel = 'noopener'
    el.classList.add('external-link')
  })
}
