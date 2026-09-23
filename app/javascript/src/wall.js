// Draw Open Wall frames onto canvases, over the cable, never from a URL.
//
// There is no <img> on a wall page any more and no address that returns image
// bytes. A canvas carries `data-wall-frame="<id>"` and `data-wall-variant`,
// this asks the channel for those ids, and the reply is painted here. A client
// that does not run JavaScript sees the <noscript> line the views carry and no
// picture, which is deliberate: the previous attempt left a plain link beside
// the scripted path for non-JS readers and the crawler simply followed it.
//
// Shaped after src/heif-viewer.js and heif.js, which already do fetch ->
// decode -> drawImage on this site.

import { createConsumer } from '@rails/actioncable'

// One consumer for the document. Turbo swaps the body, not the socket, so
// re-subscribing per navigation would open a connection per page view for no
// gain.
let consumer = null
let subscription = null
let pending = new Map()
let connected = false

function canvasesIn(root) {
  return Array.from(root.querySelectorAll('canvas[data-wall-frame]'))
}

// Say what is wrong, in the page, rather than leaving a grid of blank squares.
//
// Two ways to get here and the second is why this exists. The channel can
// refuse -- a frame budget reached -- and a socket can simply never open,
// which is what happens to a reader behind a mirror whose nginx does not
// forward the Upgrade. openipc.kz and openipc.cloud are in exactly that state
// today, and a blank gallery there would be indistinguishable from an empty
// one. A visitor is owed the difference.
function showUnavailable(reason) {
  const holder = document.querySelector('[data-wall-status]')
  if (!holder || holder.dataset.wallShown) return

  holder.dataset.wallShown = '1'
  holder.hidden = false
  holder.textContent = holder.dataset.wallStatus.replace('%{reason}', reason || '')
}

// The mask is the server's, and is obfuscation rather than secrecy -- see the
// comment on WallChannel#transmit_frame. Same key, same operation.
function unmask(bytes, key) {
  const out = new Uint8Array(bytes.length)
  for (let i = 0; i < bytes.length; i++) out[i] = bytes[i] ^ key[i % key.length]
  return out
}

function keyFor(connectionId) {
  return Array.from(connectionId, (c) => c.charCodeAt(0))
}

async function paint(canvas, bytes) {
  const blob = new Blob([bytes], { type: 'image/jpeg' })
  let bitmap
  try {
    bitmap = await createImageBitmap(blob)
  } catch {
    // A frame that will not decode leaves the canvas as it is: sized, with
    // its background, and carrying its aria-label. Better than a broken-image
    // glyph, and it is what a purged snapshot looks like.
    return
  }

  // The tiles are letterboxed against a background rather than cropped, which
  // is what the `h-100 w-auto` img classes did before.
  canvas.width = bitmap.width
  canvas.height = bitmap.height
  canvas.getContext('2d').drawImage(bitmap, 0, 0)
  canvas.dataset.wallPainted = '1'
  bitmap.close?.()
}

// id alone is not a key. One snapshot appears on a page at two sizes -- the
// fullhd hero and the first icon2 tile of its own archive strip are the same
// id -- so keying on it let whichever reply arrived first paint both canvases,
// drawing the hero from a 240x135 thumbnail.
const slot = (id, variant) => `${id}|${variant}`

function onFrame(data) {
  if (data.error) {
    showUnavailable(data.error)
    return
  }

  const canvases = pending.get(slot(data.id, data.variant))
  if (!canvases) return

  const raw = Uint8Array.from(atob(data.frame), (c) => c.charCodeAt(0))
  const bytes = unmask(raw, keyFor(data.connection_id))
  canvases.forEach((canvas) => paint(canvas, bytes))
  pending.delete(slot(data.id, data.variant))
}

function request(root) {
  const canvases = canvasesIn(root)
  if (canvases.length === 0) return

  // Group by variant: one message per variant, every id the page needs.
  const byVariant = new Map()
  canvases.forEach((canvas) => {
    const id = canvas.dataset.wallFrame
    const variant = canvas.dataset.wallVariant || 'thumb'
    if (!byVariant.has(variant)) byVariant.set(variant, new Set())
    byVariant.get(variant).add(id)

    const key = slot(id, variant)
    if (!pending.has(key)) pending.set(key, [])
    pending.get(key).push(canvas)
  })

  byVariant.forEach((ids, variant) => {
    subscription.perform('request_frames', { variant, ids: Array.from(ids) })
  })
}

export default function initWall() {
  document.addEventListener('turbo:load', () => hydrate(document))
  document.addEventListener('turbo:frame-load', (event) => hydrate(event.target))

  // Turbo caches the page on the way out and restores it on the way back. A
  // painted canvas does NOT survive that -- the cached copy is markup, and the
  // pixels are gone -- so the ids have to be asked for again. Clearing the
  // pending map here stops a stale canvas reference being painted into a
  // detached document.
  document.addEventListener('turbo:before-cache', () => {
    pending = new Map()
    canvasesIn(document).forEach((canvas) => delete canvas.dataset.wallPainted)
  })
}

function hydrate(root) {
  if (canvasesIn(root).length === 0) return

  if (!consumer) {
    consumer = createConsumer('/api/v1/wall/cable')
    subscription = consumer.subscriptions.create('WallChannel', {
      received: onFrame,
      connected: () => { connected = true; request(document) },
      disconnected: () => { connected = false },
    })

    // A handshake that never completes produces no event to hang this on --
    // it just stays silent -- so the only way to notice is to look.
    setTimeout(() => { if (!connected) showUnavailable('') }, 8000)
    return
  }

  request(root)
}
