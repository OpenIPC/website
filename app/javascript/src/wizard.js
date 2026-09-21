// The download step's second sentence, after the third image in one tab (#190).
//
// Someone assembling images for one camera and someone assembling them for a
// rack of boards want different conversations, and the only honest signal the
// site has for which is which is how many images this tab has asked for.
//
// sessionStorage, not a cookie and not the server: it is per tab, it is gone
// when the tab closes, it never leaves the browser, and it is not an
// identifier. The server route is closed anyway -- after #155 and #156 the
// wizard result is a cacheable GET, so a variant chosen per visitor would make
// the page uncacheable for everyone.
//
// The alternative wording is already in the markup as data attributes, so this
// swaps text that the server rendered and translated. Nothing here builds a
// sentence, which is what keeps it out of the three locales' way.
const KEY = 'openipc_images'
const SEEN = 'openipc_whatnext'
const FROM = 3

// A count that cannot be read is a count that does not change the page: Safari
// in private mode throws on both read and write, and the wizard has to keep
// working there. Every path returns the unchanged page rather than an error.
function bump() {
  try {
    const next = (parseInt(window.sessionStorage.getItem(KEY), 10) || 0) + 1
    window.sessionStorage.setItem(KEY, String(next))
    return next
  } catch {
    return 0
  }
}

function read() {
  try {
    return parseInt(window.sessionStorage.getItem(KEY), 10) || 0
  } catch {
    return 0
  }
}

// Whether the what-next list has already been shown in this tab. Counted
// separately from the images: a visitor can reach the result page, read the
// list and never download, and should not meet it again on the next chip.
function seen() {
  try {
    return window.sessionStorage.getItem(SEEN) === '1'
  } catch {
    return false
  }
}

// The ask is one <p>: "<question> <link>." -- so the question is the text node
// before the link and the link carries its own replacement. Rewriting both
// keeps the sentence and its full stop intact without re-rendering anything.
function applyVolumeWording(root) {
  if (read() < FROM - 1) return

  const link = (root || document).querySelector('.download-licence a[data-volume-link]')
  if (!link) return

  const question = link.previousSibling
  if (question && question.nodeType === Node.TEXT_NODE) {
    question.textContent = `${link.dataset.volumeText} `
  }
  link.textContent = link.dataset.volumeLink

  const url = new URL(link.href, window.location.origin)
  url.searchParams.set('volume', '1')
  link.href = `${url.pathname}${url.search}`
}

// The what-next list, once per tab (#191).
//
// The success block always renders -- the congratulations belong to every
// camera. The list of things to do next does not: someone flashing a batch of
// eight boards meets it eight times, and an ask that repeats on every board
// stops reading as a suggestion and starts reading as a toll, which is the
// exact failure this list was added to avoid.
//
// Hidden rather than never rendered, because the page is a cacheable GET since
// #155 and #156: one HTML body serves everyone, and which camera of a session
// this is cannot be a server-side decision without giving that up.
//
// `hidden` rather than a style, so it stays hidden for a reader who overrides
// page CSS, and so nothing has to know what display value to put back.
function applyWhatNext(root) {
  const list = (root || document).querySelector('[data-whatnext]')
  if (!list) return

  if (seen()) {
    list.hidden = true
    return
  }

  // Seen, not downloaded: arriving at the result page is what the list
  // answers, so a visitor who reads it and does not flash anything is not
  // shown it again either.
  try {
    window.sessionStorage.setItem(SEEN, '1')
  } catch {
    // No storage, so it shows every time. That is the safe direction.
  }
}

export default function initWizard() {
  applyVolumeWording()
  applyWhatNext()

  // Delegated from `document` and bound once, for the same reason the event
  // counter is: Turbo swaps the body on every navigation but never the
  // document, and re-running a bind is how one click gets counted twice.
  document.addEventListener('click', ev => {
    const link = ev.target.closest('a[href*="download_full_image"]')
    if (link) bump()
  })

  document.addEventListener('turbo:load', () => {
    applyVolumeWording()
    applyWhatNext()
  })
}
