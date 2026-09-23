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

// Bumped whenever the page changes or the socket drops. Chunk timers carry the
// generation they were scheduled under and stop if it has moved on.
//
// Both halves of that are bugs found in review. Without it, a Turbo navigation
// left the remaining chunks of the page you just LEFT still arriving, spending
// the connection's hourly budget on frames nobody is looking at; and a
// reconnect re-requested nothing, because `request` skips an id whose slot is
// already in `pending` and a dropped socket leaves every unresolved slot
// sitting there for ever.
let generation = 0

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
// comment on WallChannel#transmit_frame.
//
// Only the first MASK_BYTES are masked, and this number MUST equal
// WallChannel::MASK_BYTES. Masking the whole frame cost the server a quarter
// of a million Ruby iterations per fullhd image; the head is where a JPEG's
// markers and quantisation tables live, which is all that needs corrupting
// for the bytes not to be a picture. A channel test asserts the round trip
// rather than trusting these two constants to stay in step.
const MASK_BYTES = 4096

function unmask(bytes, key) {
  const out = new Uint8Array(bytes)
  const end = Math.min(MASK_BYTES, bytes.length)
  for (let i = 0; i < end; i++) out[i] = bytes[i] ^ key[i % key.length]
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

// Frames per message.
//
// Not the 96 the channel allows, and the difference matters on the one-day
// slideshow. A camera's day is up to 96 full-HD frames at a median 320 KB --
// about 27 MB, half again as much base64-encoded -- and asking for all of it
// in one message meant the page showed nothing much for the first few seconds
// and took around thirty to fill. Measured on production 2026-09-23: 15
// painted at 5 s, 61 at 15 s, 83 at 30 s.
//
// In chunks, in document order, the slides a reader is about to see arrive
// first and the rest stream in behind. The total is no faster; what changes is
// that the carousel -- which advances every three seconds and takes four
// minutes to cycle -- is never waiting on frames it does not need yet.
const CHUNK = 8

function request(root) {
  const canvases = canvasesIn(root)
  if (canvases.length === 0) return

  // Group by variant, preserving document order: one message per variant per
  // chunk, and the first chunk is what the reader is looking at.
  const byVariant = new Map()
  canvases.forEach((canvas) => {
    const id = canvas.dataset.wallFrame
    const variant = canvas.dataset.wallVariant || 'thumb'
    if (!byVariant.has(variant)) byVariant.set(variant, [])

    const key = slot(id, variant)
    if (!pending.has(key)) {
      pending.set(key, [])
      byVariant.get(variant).push(id)
    }
    pending.get(key).push(canvas)
  })

  byVariant.forEach((ids, variant) => sendChunks(variant, ids))
}

// Sequential rather than all at once, so one page cannot monopolise the socket
// and a reader sees the top of the page while the bottom is still arriving.
function sendChunks(variant, ids) {
  const mine = generation
  let at = 0
  const next = () => {
    // Stop if the page moved on or the socket dropped while this was queued.
    if (at >= ids.length || !subscription || mine !== generation) return
    subscription.perform('request_frames', { variant, ids: ids.slice(at, at + CHUNK) })
    at += CHUNK
    if (at < ids.length) setTimeout(next, 250)
  }
  next()
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
    generation++
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
      disconnected: () => {
        connected = false
        // Anything still waiting will never arrive on this socket, so drop the
        // slots: the reconnect's request() has to be able to ask again.
        generation++
        pending = new Map()
      },
    })

    // A handshake that never completes produces no event to hang this on --
    // it just stays silent -- so the only way to notice is to look.
    setTimeout(() => { if (!connected) showUnavailable('') }, 8000)
    return
  }

  request(root)
}
