import { decodeHeifToCanvas } from '../heif'

// Progressive enhancement: view the *original* HEIF snapshot decoded in the
// browser. The page otherwise shows a server-rendered JPEG, because HEIF is
// decodable natively only by Safari. See heif.js for the decoder itself.
export default function initHeifViewer() {
  document.addEventListener('click', async ev => {
    const btn = ev.target.closest('[data-heif-view]')
    if (!btn) return

    const wrap = btn.closest('[data-heif-original]')
    const url = wrap && wrap.dataset.heifOriginal
    const canvas = wrap && wrap.querySelector('[data-heif-canvas]')
    const status = wrap && wrap.querySelector('[data-heif-status]')
    if (!url || !canvas) return

    btn.disabled = true
    if (status) status.textContent = 'Decoding…'
    try {
      const buffer = await (await fetch(url, { credentials: 'same-origin' })).arrayBuffer()
      const how = await decodeHeifToCanvas(buffer, canvas)
      canvas.classList.remove('d-none')
      btn.classList.add('d-none')
      if (status) status.textContent = `Original HEIF, decoded in your browser (${how}).`
    } catch (e) {
      console.warn('HEIF decode failed', e)
      if (status) status.textContent = 'Your browser can’t decode this HEIF — showing the server preview.'
      btn.disabled = false
    }
  })
}
