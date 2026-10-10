/**
 * The site's photo viewer: a full-screen gallery over every picture of one
 * set (a board's photos and pinouts, a page's screenshots), opened on the one
 * clicked. Arrows and the arrow keys move through the set; a swipe does on a
 * phone, a pinch or a double tap zooms and a drag pans, which is what a
 * pinout's pin table needs; a swipe down or Escape closes it.
 *
 * PhotoSwipe does the gestures. It is loaded the first time a picture is
 * opened, never with the page, and it opens inside the topmost modal
 * <dialog> when there is one (the board panel): a dialog is in the top layer,
 * and anything outside it renders underneath.
 *
 * Back closes the viewer rather than leaving the page, as a phone's back
 * gesture is expected to: opening pushes a history entry with the address
 * unchanged, and closing by any other way takes it back off.
 */
import type PhotoSwipe from 'photoswipe';

export interface GalleryItem {
  /** The full-size picture. */
  src: string;
  /** Its size in pixels: how far it can be zoomed, and its shape before it has loaded. */
  width: number;
  height: number;
  /** A smaller copy already on the page, shown at once while the full one loads. */
  thumb?: string;
  alt: string;
  /** One line under the picture: what it shows. */
  caption?: string;
}

export interface GalleryLabels {
  close: string;
  zoom: string;
  prev: string;
  next: string;
  original: string;
  error: string;
  /** Between the index and the count: "3 / 7". */
  of: string;
}

const LABELS_ID = 'gallery-labels';

const FALLBACK: GalleryLabels = {
  close: 'Close', zoom: 'Zoom', prev: 'Previous', next: 'Next', original: 'Open the original', error: 'The picture could not be loaded.', of: ' / ',
};

/** The labels the page carries in its own language (ZoomDialog.astro writes them). */
export function pageLabels(): GalleryLabels {
  const el = typeof document === 'undefined' ? null : document.getElementById(LABELS_ID);
  if (!el?.textContent) return FALLBACK;
  try {
    return { ...FALLBACK, ...(JSON.parse(el.textContent) as Partial<GalleryLabels>) };
  } catch {
    return FALLBACK;
  }
}

const ORIGINAL_ICON = '<svg aria-hidden="true" class="pswp__icn" viewBox="0 0 32 32" width="32" height="32">'
  + '<path d="M18 7h7v7M25 7l-9 9M14 9H9a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';

let open: PhotoSwipe | null = null;

// Forward onto a viewer's history entry after Back closed it finds nothing to
// show: step back over it, so it is never a dead stop on the way off the page.
if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    if (!open && (window.history.state as { gallery?: boolean } | null)?.gallery) window.history.back();
  });
}

/**
 * Open the set at `index`. Resolves once the viewer is showing; a second call
 * while one is open replaces nothing and is ignored.
 */
export async function openGallery(items: GalleryItem[], index = 0, labels: GalleryLabels = pageLabels()): Promise<void> {
  if (open || items.length === 0) return;
  // The dialog the picture was clicked in, chosen before the first open's
  // download: if it closes meanwhile, so does the reason to show its photos.
  const dialog = [...document.querySelectorAll<HTMLDialogElement>('dialog[open]')].pop() ?? null;
  // Its stylesheet (2 KB) is in app.css: a separate chunk would load on first open, a frame late.
  const { default: PhotoSwipeCore } = await import('photoswipe');
  if (open || (dialog && !(dialog.isConnected && dialog.open))) return;

  const host = dialog ?? document.body;
  const pswp = new PhotoSwipeCore({
    dataSource: items.map((it) => ({ src: it.src, width: it.width, height: it.height, msrc: it.thumb, alt: it.alt, caption: it.caption })),
    index: Math.max(0, Math.min(index, items.length - 1)),
    appendToEl: host,
    // Black, not a dimmed page: the panel's text behind a pinout is noise.
    bgOpacity: 1,
    // Room for the caption under the picture, and on a wide screen for the bar
    // and the arrows around it; a phone gives the picture its whole width.
    paddingFn: (viewport) => (viewport.x < 640
      ? { top: 56, bottom: 48, left: 0, right: 0 }
      : { top: 64, bottom: 56, left: 72, right: 72 }),
    showHideAnimationType: 'fade',
    // A picture is shown whole; a tap or a double tap goes to full resolution.
    initialZoomLevel: 'fit',
    secondaryZoomLevel: 1,
    maxZoomLevel: 4,
    preload: [1, 2],
    closeOnVerticalDrag: true,
    returnFocus: true,
    closeTitle: labels.close,
    zoomTitle: labels.zoom,
    arrowPrevTitle: labels.prev,
    arrowNextTitle: labels.next,
    errorMsg: labels.error,
    indexIndicatorSep: labels.of,
  });
  open = pswp;

  pswp.on('uiRegister', () => {
    pswp.ui?.registerElement({
      name: 'original',
      order: 8,
      isButton: true,
      tagName: 'a',
      title: labels.original,
      ariaLabel: labels.original,
      html: ORIGINAL_ICON,
      onInit: (el) => {
        const a = el as HTMLAnchorElement;
        a.target = '_blank';
        a.rel = 'noopener';
        const set = () => { a.href = String(pswp.currSlide?.data.src ?? ''); };
        pswp.on('change', set);
        set();
      },
    });
    pswp.ui?.registerElement({
      name: 'caption',
      order: 9,
      isButton: false,
      appendTo: 'root',
      onInit: (el) => {
        el.className = 'pswp__caption';
        el.setAttribute('aria-live', 'polite');
        const set = () => {
          const text = (pswp.currSlide?.data as { caption?: string } | undefined)?.caption ?? '';
          el.textContent = text;
          el.hidden = !text;
        };
        pswp.on('change', set);
        set();
      },
    });
  });

  // A request to close the dialog under the viewer -- Escape, or a phone's
  // Back, which Chrome on Android sends a modal dialog as `cancel` rather than
  // as a step back in history -- closes the viewer and leaves the dialog open.
  const keepDialog = (e: Event) => { e.preventDefault(); pswp.close(); };
  // And the dialog's own scrollbar is not drawn over the picture.
  const overflow = host.style.overflow;
  if (host instanceof HTMLDialogElement) {
    host.addEventListener('cancel', keepDialog);
    host.style.overflow = 'hidden';
  }

  // Back closes the viewer. The entry carries the page's own state, so a page
  // that reads history.state (the board catalogue's panel depth) sees no change.
  let viaBack = false;
  const onPop = () => { viaBack = true; pswp.close(); };
  window.history.pushState({ ...(window.history.state ?? {}), gallery: true }, '');
  window.addEventListener('popstate', onPop);

  pswp.on('destroy', () => {
    window.removeEventListener('popstate', onPop);
    if (host instanceof HTMLDialogElement) {
      host.removeEventListener('cancel', keepDialog);
      host.style.overflow = overflow;
    }
    if (!viaBack && (window.history.state as { gallery?: boolean } | null)?.gallery) window.history.back();
    open = null;
  });

  pswp.init();
}

/**
 * The set a picture on a plain page belongs to: every img[data-zoom] inside
 * its nearest [data-zoom-group], or on the whole page when it has none.
 */
export function domGroup(img: HTMLImageElement): { items: GalleryItem[]; index: number } {
  const group = img.closest('[data-zoom-group]');
  const imgs = group
    ? [...group.querySelectorAll<HTMLImageElement>('img[data-zoom]')]
    : [...document.querySelectorAll<HTMLImageElement>('img[data-zoom]')].filter((el) => !el.closest('[data-zoom-group]'));
  const items = imgs.map((el) => {
    const w = Number(el.dataset.zoomWidth) || el.naturalWidth || 1600;
    const h = Number(el.dataset.zoomHeight) || el.naturalHeight || 1200;
    return { src: el.dataset.zoom ?? el.src, width: w, height: h, thumb: el.currentSrc || el.src, alt: el.alt, caption: el.dataset.zoomCaption ?? el.alt };
  });
  return { items, index: Math.max(0, imgs.indexOf(img)) };
}
