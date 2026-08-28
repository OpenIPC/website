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
import 'bootstrap/js/dist/carousel'

import initZoom from './src/zoom'
import initExternalLinks from './src/external-links'
import initTimestamps from './src/timestamps'
import initConfirms from './src/confirms'
import initHeifViewer from './src/heif-viewer'

// DOMContentLoaded, not window.onload, which waits for every image and on the
// Open Wall meant the page sat unresponsive until the whole gallery had loaded.
// Assigning window.onload was also a single slot: a second assignment anywhere
// would have silently replaced all of this.
document.addEventListener('DOMContentLoaded', () => {
  initZoom()
  initExternalLinks()
  initTimestamps()
  initConfirms()
  initHeifViewer()
})
