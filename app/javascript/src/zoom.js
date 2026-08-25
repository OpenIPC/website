import Modal from 'bootstrap/js/dist/modal'

// Click-to-zoom for images marked .img-zoom; needs the #modalZoom markup.
export default function initZoom() {
  const modalZoom = document.getElementById('modalZoom')
  // Most pages have no zoomable image and therefore no modal. Constructing a
  // Modal on null throws, and the throw took every later initialiser on the
  // page down with it, because they all ran from one window.onload handler.
  if (!modalZoom) return

  const zoom = new Modal(modalZoom, {})

  // Delegated, so images added after load still zoom.
  document.addEventListener('click', ev => {
    const img = ev.target.closest('.img-zoom')
    if (!img) return

    const body = modalZoom.querySelector('.modal-body')
    body.textContent = ''
    const full = document.createElement('img')
    // The tile is a downscaled copy; data-zoom, where present, names the
    // full-resolution file so the modal is not an upscale of the thumbnail.
    full.src = img.dataset.zoom || img.src
    full.classList.add('img-fluid')
    body.appendChild(full)
    zoom.show()
  })
}
