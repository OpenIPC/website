import Modal from 'bootstrap/js/dist/modal'

// Click-to-zoom for images marked .img-zoom; needs the #modalZoom markup.
//
// Bound once, from application.js, and everything it needs is looked up when a
// click actually happens. It used to find #modalZoom at startup and build the
// Modal there, which cannot survive Turbo: the element belongs to the page, so
// the instance goes stale on the first navigation, and a page without a
// zoomable image returned early and left the handler unbound for every page
// after it. Binding per navigation instead would have stacked a fresh listener
// each time and opened the modal twice, then three times.
export default function initZoom() {
  // Delegated, so images added after load still zoom.
  document.addEventListener('click', ev => {
    const img = ev.target.closest('.img-zoom')
    if (!img) return

    // Most pages have no zoomable image and therefore no modal. Constructing a
    // Modal on null throws, and a throw in here used to take every later
    // initialiser down with it, because they all ran from one handler.
    const modalZoom = document.getElementById('modalZoom')
    if (!modalZoom) return

    const body = modalZoom.querySelector('.modal-body')
    body.textContent = ''
    const full = document.createElement('img')
    // The tile is a downscaled copy; data-zoom, where present, names the
    // full-resolution file so the modal is not an upscale of the thumbnail.
    full.src = img.dataset.zoom || img.src
    full.classList.add('img-fluid')
    body.appendChild(full)
    // getOrCreateInstance, not new: after a Turbo navigation the element is a
    // different one, and Bootstrap keys its instances on the element.
    Modal.getOrCreateInstance(modalZoom, {}).show()
  })
}
