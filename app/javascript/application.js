// Turbo Drive: a click on an internal link fetches the new page and swaps the
// body instead of tearing the document down and building it again. The gems
// have been in the Gemfile since the app was generated, but nothing ever
// imported them, so every navigation was a full document load -- which is what
// "CGI-ish" meant, and it was accurate.
import '@hotwired/turbo-rails'

// Bootstrap's JS, imported per-component rather than as `import * as bootstrap`.
// Each of these registers Bootstrap's data API on import, so the markup keeps
// working with no further wiring.
//
// The list is exactly what the views use: data-bs-toggle asks for dropdown,
// collapse and offcanvas, the Open Wall slideshow uses data-bs-ride, and
// src/zoom.js imports Modal directly (which is also what serves the one
// data-bs-dismiss="modal"). Nothing uses tooltip, popover, tab, alert or
// scrollspy. Adding markup that needs one of those means adding the import.
//
// This is a smaller win than it looks -- 189.4 KB to 181.8 KB unminified,
// because the components share most of their base. It is worth doing for the
// list above, which is a statement of what the site actually depends on.
import 'bootstrap/js/dist/collapse'
import 'bootstrap/js/dist/dropdown'
import 'bootstrap/js/dist/offcanvas'
import Carousel from 'bootstrap/js/dist/carousel'

import initZoom from './src/zoom'
import initExternalLinks from './src/external-links'
import initTimestamps from './src/timestamps'
import initConfirms from './src/confirms'
import initHeifViewer from './src/heif-viewer'
import initCopy from './src/copy'

// Forms are left alone, deliberately.
//
// Turbo expects a form submission to answer with a redirect, and refuses to do
// anything with a plain 200 -- it logs "Form responses must redirect to another
// location" and the page simply does not change. Two surfaces here answer that
// way: the installation wizard, whose update action renders the instructions
// directly because it persists nothing, and every Devise form, which re-renders
// itself with a 200 when a field is wrong.
//
// So Drive handles links, which is the whole of the complaint, and forms submit
// exactly as they did yesterday. Turning this on for a form is then a decision
// per form rather than a site-wide gamble -- and the wizard is due to become a
// GET anyway (#156), at which point it is a link and this stops applying to it.
// `Turbo.config` is the current spelling; `setFormMode` still works in Turbo 8
// but logs a deprecation warning on every single page load, which drowns the
// console output anyone debugging this site is trying to read.
if (window.Turbo.config) {
  window.Turbo.config.forms.mode = 'off'
} else {
  window.Turbo.setFormMode('off')
}

// Bound once, at import. These delegate from `document`, which Turbo never
// replaces, so they keep working across navigations -- and binding them again
// per page would add a second listener each time, firing the copy or the zoom
// twice, then three times.
initZoom()
initCopy()
initHeifViewer()

// Re-run per page. These walk the DOM and attach to the elements they find, so
// they have to run again once Turbo has swapped in new ones. turbo:load fires
// on the first load as well as after every navigation, which is why it replaces
// DOMContentLoaded rather than joining it: DOMContentLoaded fires only for the
// document Turbo started with.
document.addEventListener('turbo:load', () => {
  initExternalLinks()
  initTimestamps()
  initConfirms()

  // Bootstrap starts `data-bs-ride` carousels from its own `load` listener,
  // which a Turbo navigation never fires -- so the Open Wall's one-day
  // slideshow would sit on its first frame for anyone who arrived by clicking
  // rather than by typing the URL. getOrCreateInstance is idempotent, so this
  // is safe to run on every navigation and does nothing on the pages without
  // a carousel.
  document.querySelectorAll('[data-bs-ride="carousel"]')
          .forEach(el => Carousel.getOrCreateInstance(el))
})

// ...and stop them again on the way out. A carousel cycles on a setInterval
// that Bootstrap only clears when the instance is disposed, and Turbo never
// disposes anything: it swaps the body and leaves the old elements detached
// with their timers still running. Measured on /snapshots/:id/oneday -- one
// live interval on the page, still live after navigating home, and one more
// added by every subsequent visit for the rest of the session.
document.addEventListener('turbo:before-cache', () => {
  document.querySelectorAll('[data-bs-ride="carousel"]')
          .forEach(el => Carousel.getInstance(el)?.dispose())
})
