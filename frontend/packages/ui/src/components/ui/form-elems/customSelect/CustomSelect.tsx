import type {SelectProps} from './select-types';
import Icons from '../../../../assets/icons/ui';
import {useEffect, useRef, useState} from 'preact/hooks';

export default function CustomSelect ({state, value, onChange, options, open, size, elemName, label, description}: SelectProps) {
  const {ArrowDown} = Icons;
  const [isOpen, setIsOpen] = useState(open ?? false);
  const [display, setDisplay] = useState(value);
  const valueInputRef = useRef<HTMLInputElement>(null);

  function handleOptionClick (value: SelectProps['options'][number]['value'], disabled: boolean) {
    if (disabled) return;
    setDisplay(value);
    if (isOpen) setIsOpen(false);
    if (valueInputRef.current) {
      valueInputRef.current.value = value;
      const onChangeEvt = new Event('input');
      valueInputRef.current.dispatchEvent(onChangeEvt);
    }
  }

  function handleInput (e: Event) {
    if (e.target instanceof HTMLInputElement) {
      onChange(e);
    }
  }

  useEffect(() => {
    const close = () => isOpen && setIsOpen(false);
    document.addEventListener('click', close);
    return () => {
      document.removeEventListener(
        'click',
        close,
      );
    };
  });

  // Mirroring a prop into state, so the box can show a pending choice before
  // the parent echoes it back through `value`.
  useEffect(
    () => {
      // eslint-disable-next-line @eslint-react/set-state-in-effect -- see above
      setDisplay(value);
    },
    [value],
  );

  // Same: `open` is a controlled prop that may also be set from inside.
  useEffect(
    () => {
      // eslint-disable-next-line @eslint-react/set-state-in-effect -- see above
      setIsOpen(open ?? false);
    },
    [open],
  );

  function selectStyleFab (state: SelectProps['state']) {
    const sizer: Record<NonNullable<SelectProps['size']>, string> = {
      sm: 'h-7',
      md: 'h-9',
      xl: 'h-11',
    };

    const baseStyle = `${sizer[size ?? 'md']} w-full px-2 pr-8 outline outline-0 border rounded hover:outline-1 focuse:outline-none flex flex-row items-center cursor-default relative truncate`;
    const styles: Record<SelectProps['state'], () => string> = {
      default: () => `${baseStyle} border-grey hover:outline-grey focus:bg-donban-bg`,
      valid: () => `${baseStyle} border-green hover:outline-green bg-input-bg-green focus:bg-input-focus-bg-green`,
      error: () => `${baseStyle} border-red hover:outline-red bg-input-bg-red focus:bg-input-focus-bg-red`,
      disabled: () => 'w-full px-2 outline outline-0 border rounded hover:cursor-default text-grey',
    };
    return styles[state]();
  }

  function getSelectBody () {
    return (
      <div className="relative w-full">
        <input className="hidden" ref={valueInputRef} onChange={handleInput} name={elemName} value={value} />
        <input
          className={selectStyleFab(state)}
          {...(state !== 'disabled' && {onClick: () => !isOpen && setIsOpen(true)})}
          readOnly={true}
          value={display}
          name={elemName}
          id={elemName}
        />
        <div className="absolute inset-y-0 right-2 flex flex-col justify-center" {...(state !== 'disabled' && {onClick: () => !isOpen && setIsOpen(true)})}>
          <ArrowDown />
        </div>
        { isOpen &&
          <ul className="
            absolute z-50 flex w-full cursor-default flex-col gap-y-px
            rounded-sm border border-grey bg-white px-1 py-2 shadow-md
          ">
            {
              options.map(({value, option, disabled}) => <li
                className={`
                  min-h-[28px] rounded-sm px-1 py-[2px]
                  ${!disabled && `hover:bg-light-blue`}
                  ${disabled && 'cursor-not-allowed text-grey'}
                `}
                onClick={(e) => {
                  e.stopPropagation(); handleOptionClick(
                    value,
                    disabled ?? false,
                  );
                }}
                key={value}
              >
                {option}
              </li>)
            }
          </ul>
        }
      </div>
    );
  }

  return (
    <div className="w-full">
      { label && <label className="text-sm">{label}</label> }
      { getSelectBody() }
      { description && <div className="text-sm">{description}</div> }
    </div>
  );
}
