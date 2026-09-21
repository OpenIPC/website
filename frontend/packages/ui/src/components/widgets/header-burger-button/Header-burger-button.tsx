type PropsType = {
  clickHandler: () => void;
  isOpen: boolean;
}

export default function HeaderBurgerButton(props: PropsType) {
  const { clickHandler, isOpen } = props;

  function handleClick(evt: MouseEvent) {
    evt.preventDefault();
    clickHandler();
  }

  return (
    <div 
      className="
        relative m-0 h-[22px] w-[29px] cursor-pointer opacity-80 transition-all
        duration-500 ease-in-out
        hover:opacity-100
      "
      onClick={handleClick}
    >
      <span className={`
        absolute block h-[4px] w-full origin-left rounded-[2px] bg-white
        shadow-[0_3px_2px_rgba(0,0,0,0.2)] transition-all duration-200
        ease-in-out
        ${isOpen ? 'top-[-3px] left-0 rotate-45' : 'top-0'}
      `}></span>
      
      <span className={`
        absolute top-[9px] block h-[4px] w-full rounded-[2px] bg-white
        shadow-[0_3px_2px_rgba(0,0,0,0.2)] transition-all duration-200
        ease-in-out
        ${isOpen ? 'w-0 opacity-0' : ''}
      `}></span>
      
      <span className={`
        absolute block h-[4px] w-full origin-left rounded-[2px] bg-white
        shadow-[0_3px_2px_rgba(0,0,0,0.2)] transition-all duration-200
        ease-in-out
        ${isOpen ? 'top-[18px] left-0 -rotate-45' : 'top-[18px]'}
      `}></span>
    </div>
  );
}
