import { useEffect, useState } from 'preact/hooks';

export function useMediaQuery(query: string): boolean {
  // Server rendering has no matchMedia. Start false and let the effect below
  // correct it on hydration, rather than throwing during the prerender.
  const [ matches, setMatches ] = useState<boolean>(() =>
    typeof window === 'undefined' ? false : window.matchMedia(query).matches
  );

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
