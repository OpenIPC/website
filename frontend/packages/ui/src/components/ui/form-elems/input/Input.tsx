import type { InputProps } from './Input-types';
import { useRef } from 'preact/hooks';

export default function Input(props: InputProps) {
  const inputRef = useRef(null);
  const {
    elemName, type, label, state,
    onInput, required, value, placeholder,
    Icon, iconClickHandler, iconPlace,
    iconTooltip, errorText, description
  } = props;

  const getUnderInputText = (errorText?: string, description?: string) => {
    return errorText
      ? errorText
      : description
        ? description
        : '';
  }

  const inputStyleFab = (state: InputProps['state']) => {
    const baseStyle = 'h-9 w-full px-2 outline outline-0 border rounded hover:outline-1 focuse:outline-none';
    const styles: Record<InputProps['state'], () => string> = {
      'default': () => `${baseStyle} border-grey hover:outline-grey focus:bg-donban-bg`,
      'valid': () => `${baseStyle} border-green hover:outline-green bg-input-bg-green focus:bg-input-focus-bg-green`,
      'error': () => `${baseStyle} border-red hover:outline-red bg-input-bg-red focus:bg-input-focus-bg-red`,
      'disabled': () => `h-9 w-full px-2 outline outline-0 border rounded`,
    };
    return styles[state](); 
  }

  const getIconPaddings = (iconPlace: InputProps['iconPlace']) => {
    return iconPlace === 'left' ? 'pl-10' : 'pr-10';
  }

  const getIconStyle = (iconPlace: InputProps['iconPlace']) => {
    return iconPlace === 'left'
      ? 'absolute left-0 top-0 h-9 ml-2 flex flex-row items-center *:w-[24px] *:h-[24px] *:hover:cursos-pointer'
      : 'absolute right-0 top-0 h-9 mr-2 flex flex-row items-center *:w-[24px] *:h-[24px] *:hover:cursor-pointer';
  }

  function handleIconClick(e: MouseEvent) {
    if (iconClickHandler && inputRef.current) iconClickHandler(inputRef.current, e);
  }

  return (
    <div className="w-full">
      <label for={elemName} className="text-sm">{label}{required && <span className="
        pl-1 text-crimson
      ">*</span>}</label>
      <div className="relative mt-1">
        <input className={`
          ${inputStyleFab(state)}
          ${Icon && getIconPaddings(iconPlace)}
        `} disabled={state === 'disabled'}
          type={type} id={elemName} name={elemName} {...(placeholder && { placeholder })}
          {...(value && { value })} onInput={onInput} ref={inputRef}
        />
        {Icon && <div className={getIconStyle(iconPlace)} onClick={handleIconClick} title={iconTooltip}><Icon /></div>}
      </div>
      <div className="min-h-5 leading-4">
        <span className={`
          text-sm
          ${state==='error' && errorText && 'text-red'}
        `}>{getUnderInputText(errorText, description)}</span>
      </div>
    </div>
  );
}
