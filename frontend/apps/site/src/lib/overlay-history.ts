/**
 * Back closes an overlay -- the photo viewer, a console shown full screen --
 * rather than leaving the page, as a phone's back gesture is expected to.
 *
 * Opening pushes a history entry with the address unchanged, carrying the
 * page's own state (the board catalogue reads its panel depth from it), and
 * marked as an overlay's. Back off it calls `onBack`; closing any other way
 * takes the entry back off. Forward onto an overlay's entry once nothing is
 * open steps back over it, so it is never a dead stop on the way off the page.
 */

let holds = 0;

const marked = () => !!(window.history.state as { overlay?: boolean } | null)?.overlay;

if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    if (holds === 0 && marked()) window.history.back();
  });
}

/** Hold Back for an overlay now open. The function returned releases it when the overlay closes. */
export function holdBack(onBack: () => void): () => void {
  let released = false;
  let viaBack = false;
  holds++;
  window.history.pushState({ ...(window.history.state ?? {}), overlay: true }, '');
  const onPop = () => {
    if (released) return;
    viaBack = true;
    onBack();
  };
  window.addEventListener('popstate', onPop);
  return () => {
    if (released) return;
    released = true;
    holds--;
    window.removeEventListener('popstate', onPop);
    if (!viaBack && marked()) window.history.back();
  };
}
