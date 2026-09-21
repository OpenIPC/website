import type MainButtonProps from './main-button-types';

export default function MainButton({ size, disabled, caption, type, Icon, clickHandler }: MainButtonProps) {
  function getSize(size: MainButtonProps['size']) {
    const sizes:Record<MainButtonProps['size'], string> = {
      xs: 'w-16 h-8',
      s: 'w-28 h-8',
      m: 'w-60 h-8',
      l: 'w-72 h-12',
    };
    return sizes[size]; 
  }

  function getClass() {
    const baseClass = `bg-brand-blue border border-0 rounded-md flex flex-row justify-center items-center gap-x-2 text-white hover:bg-btn-blue-hover active:bg-btn-blue-click ${getSize(size)}`;
    if (disabled) return `${baseClass} bg-grey flex flex-row justify-center items-center`;
    return baseClass;
  }

  return (
    <button className={getClass()} type={type ?? 'button'} {...{disabled}} onClick={clickHandler}>
      {caption}
      {Icon && <Icon />}
    </button>
  );
}
