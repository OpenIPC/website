import type ToggleButtonProps from './toggle-button-types';

export default function ToggleButton({ size, checked, disabled, Icon, label, changeHandler }: ToggleButtonProps) {

  function getSize(size: ToggleButtonProps['size']) {
    const sizes:Record<ToggleButtonProps['size'], string> = {
      xs: 'w-16 h-8',
      s: 'w-28 h-8',
      m: 'w-60 h-8',
      l: 'w-72 h-12',
    };
    return sizes[size]; 
  }

  function handleChange(e: Event) {
    if (!(e.target instanceof HTMLInputElement)) return;
    if (changeHandler) changeHandler(e.target.checked);
  }

  function getClass() {
    const baseClass = `block border border-0 rounded-md relative overflow-hidden flex flex-row justify-center items-center ${getSize(size)}`;
    if (disabled) return `${baseClass} bg-grey flex flex-row justify-center items-center`;
    return `${baseClass} bg-brand-blue has-checked:bg-btn-blue-click has-checked:shadow-[inset_3px_3px_6px_#000051]`
  }

  return (
    <div className="max-w-min rounded-sm">
      <label className={getClass()}>
        {Icon && <Icon />}
        <input type="checkbox" aria-label={label} onChange={handleChange} {...{checked, disabled}} className="
          absolute -top-1 size-0
        " />
      </label>
    </div>
  );
}
