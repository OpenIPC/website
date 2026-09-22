import { useEffect, useState } from 'preact/hooks';

export function useMediaQuery(query: string): boolean {
  // Always false for the first render, on the server and in the browser
  // alike, then corrected by the effect below.
  //
  // Reading matchMedia in this initialiser looked harmless and was not:
  // HeaderMenu picks between structurally different desktop and mobile trees
  // from this value, so on a phone the server emitted the desktop tree and
  // the very first client render wanted the mobile one. That is a hydration
  // mismatch, and Preact resolves it by rebuilding the subtree. One frame of
  // the desktop menu is the better trade, and a caller that cannot accept
  // even that should branch in CSS rather than here.
  const [ matches, setMatches ] = useState<boolean>(false);

  useEffect(() => {
    const mediaQuery = window.matchMedia(query);
    // The whole point: the server rendered `false`, and this is the first
    // moment a real answer exists.
    // eslint-disable-next-line @eslint-react/set-state-in-effect -- see above
    setMatches(mediaQuery.matches);
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches);

    mediaQuery.addEventListener('change', handler);
    return () => mediaQuery.removeEventListener('change', handler);
  }, [query]);

  return matches;
}
