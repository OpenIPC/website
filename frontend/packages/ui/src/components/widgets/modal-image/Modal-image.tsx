import { useEffect, useRef } from 'preact/hooks';
import icons from '../../../assets/icons/ui';

/**
 * The body scroll lock is shared, because the page has one body.
 *
 * Each instance used to snapshot and restore document.body.style on its own,
 * so with two modals open, closing the first restored the page's scrolling
 * while the second was still up. A depth count means the first one in takes
 * the snapshot and the last one out puts it back.
 */
let lockDepth = 0;
let lockedStyles: { width: string, paddingRight: string, top: string, position: string } | null = null;
let lockedScrollY = 0;

export default function ModalImage({
  src, alt, close
}: { src: string, alt: string, close: () => void }) {
  const { Cross } = icons;

  // Escape used to invoke whichever close was passed on mount, for as long as
  // the modal lived, because the listener is bound once.
  const closeRef = useRef(close);
  useEffect(() => { closeRef.current = close; });
  
  function handleBackdropClick(e: Event) {
    if (
      e.target instanceof HTMLDivElement
      && e.currentTarget instanceof HTMLDivElement
      && e.currentTarget === e.target
    ) close();
  }

  function handleEscKeyPress(e: KeyboardEvent) {
    if (e.code === 'Escape') closeRef.current();
  }

  // Mount-only: it locks body scroll and binds Escape for the modal's life.
  useEffect(() => {
    document.addEventListener("keyup", handleEscKeyPress);

    const { style } = document.body;
    if (lockDepth === 0) {
      // Put back exactly what was there. Cleanup used to assign invented
      // values -- position: static, padding-right: 0 -- which is not a
      // restore: a page that styled its own body kept them.
      lockedStyles = {
        width: style.width,
        paddingRight: style.paddingRight,
        top: style.top,
        position: style.position,
      };
      lockedScrollY = window.scrollY;
      const innerWidth = window.innerWidth;
      const { right: bodyRight } = document.body.getBoundingClientRect();
      style.width = '100%';
      style.paddingRight = `${innerWidth - bodyRight}px`;
      style.top = `-${lockedScrollY}px`;
      style.position = 'fixed';
    }
    lockDepth++;

    return () => {
      document.removeEventListener("keyup", handleEscKeyPress)
      lockDepth--;
      if (lockDepth === 0 && lockedStyles) {
        style.width = lockedStyles.width;
        style.paddingRight = lockedStyles.paddingRight;
        style.position = lockedStyles.position;
        style.top = lockedStyles.top;
        lockedStyles = null;
        window.scroll(0, lockedScrollY);
      }
    }
     
  }, [])

  return (
    <div className="
      fixed top-0 left-0 z-1500 flex size-full flex-row items-center
      overflow-x-hidden overflow-y-auto overscroll-y-contain
      bg-[rgba(0,0,0,0.6)] py-8 outline-0
      md:block
    " onClick={handleBackdropClick}>
      <div className="mx-auto w-10/12 rounded-lg border bg-white">
        <div className="flex flex-row rounded-t-lg border-b-2 bg-white p-4">
          <p className="w-[calc(100%-24px)] truncate text-lg text-brand-blue">{alt}</p>
          <div className="
            size-6
            *:size-6 *:fill-light-blue *:transition-all
            hover:cursor-pointer
            hover:*:fill-brand-blue
          " onClick={close}>
            <Cross />
          </div>
        </div>
        <div className="p-4">
          <img className="mx-auto" src={src} alt={alt}/>
        </div>
      </div>
    </div>
  );
}
