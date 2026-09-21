import type { SelectProps } from './select-types';

export default function Select({ elemName, label, state, onInput, options, required, description }: SelectProps) {
  function selectStyleFab(state: SelectProps['state']) {
    const baseStyle = 'h-9 w-full px-2 outline outline-0 border rounded hover:outline-1 focuse:outline-none';
    const styles: Record<SelectProps['state'], () => string> = {
      'default': () => `${baseStyle} border-grey hover:outline-grey focus:bg-donban-bg`,
      'valid': () => `${baseStyle} border-green hover:outline-green bg-input-bg-green focus:bg-input-focus-bg-green`,
      'error': () => `${baseStyle} border-red hover:outline-red bg-input-bg-red focus:bg-input-focus-bg-red`,
      'disabled': () => `h-9 w-full px-2 outline outline-0 border rounded`,
    };
    return styles[state](); 
  }

  return (
    <div className="w-full">
      <label for={elemName} className="text-sm">{label}{required && <span className="
        pl-1 text-crimson
      ">*</span>}</label>
      <select className={`
        ${selectStyleFab(state)}
      `} onInput={onInput} disabled={state === 'disabled'}>
        {options.map(({value, disabled}) => <option key={value} value={value} {...(disabled && {disabled: disabled})}>{value}</option>)}
      </select>
      <div>
        <span className="text-sm">{description ?? ''}</span>
      </div>
    </div>
  );
}
