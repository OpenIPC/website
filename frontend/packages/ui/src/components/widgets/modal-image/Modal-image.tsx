import {useEffect} from 'preact/hooks';
import icons from '../../../assets/icons/ui';

export default function ModalImage({
  src, alt, close
}: { src: string, alt: string, close: () => void }) {
  const { Cross } = icons;
  
  function handleBackdropClick(e: Event) {
    if (
      e.target instanceof HTMLDivElement
      && e.currentTarget instanceof HTMLDivElement
      && e.currentTarget === e.target
    ) close();
  }

  function handleEscKeyPress(e: KeyboardEvent) {
    if (e.code === 'Escape') close();
  }

  // Mount-only: it locks body scroll and binds Escape for the modal's life.
  useEffect(() => {
    document.addEventListener("keyup", handleEscKeyPress);
    const innerWidth = window.innerWidth;
    const { right: bodyRight } = document.body.getBoundingClientRect();
    document.body.style.width = '100%';
    document.body.style.paddingRight = `${innerWidth - bodyRight}px`;
    document.body.style.top = `-${window.scrollY}px`;
    document.body.style.position = 'fixed';
    return () => {
      document.removeEventListener("keyup", handleEscKeyPress)
      const scrollY = document.body.style.top;
      document.body.style.width = '';
      document.body.style.position = 'static';
      document.body.style.setProperty('padding-right', '0');
      document.body.style.top = '';
      window.scroll(0, parseInt(scrollY || '0') * -1);
    }
    // eslint-disable-next-line @eslint-react/exhaustive-deps -- see above
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
