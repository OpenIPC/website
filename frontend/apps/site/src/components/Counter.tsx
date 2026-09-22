import { useState } from 'preact/hooks';
import { MainButton } from '@openipc/ui';

/**
 * The island. Nothing more than proof that a @openipc/ui component hydrates:
 * the count only moves once client JavaScript has run, so a page that renders
 * this and never counts has a broken island rather than a broken build.
 */
export default function Counter() {
  const [clicks, setClicks] = useState(0);
  return (
    <div class="flex flex-row items-center gap-3">
      <MainButton size="s" caption={`Clicked ${clicks}`} clickHandler={() => setClicks(clicks + 1)} />
      <span class="text-xs text-dark-grey">
        {clicks === 0 ? 'hydrated when this button responds' : 'island is live'}
      </span>
    </div>
  );
}
