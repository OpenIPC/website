import type {SelectProps} from './select-types';
import Icons from '../../../../assets/icons/ui';
import {useEffect, useRef, useState} from 'preact/hooks';

export default function CustomSelect ({state, value, onChange, options, open, size, elemName, label, description}: SelectProps) {
  const {ArrowDown} = Icons;
  const [isOpen, setIsOpen] = useState(open ?? false);
  // Seeded through displayFor, not from the raw value: the effect that
  // corrects it does not run on the server, so a prerendered page showed 8
  // where 'NOR 8' was configured.
  const [display, setDisplay] = useState(
    () => options.find((o) => o.value === value)?.display ?? value,
  );
  const valueInputRef = useRef<HTMLInputElement>(null);

  /** What the closed box shows for a value: the option's `display`, not the
   *  value itself, which is why 8 was appearing where 'NOR 8' was meant. */
  const displayFor = (v: string) =>
    options.find((o) => o.value === v)?.display ?? v;

  function handleOptionClick (value: SelectProps['options'][number]['value'], disabled: boolean) {
    // The control as a whole, not just the option: a select mounted open,
    // or disabled while open, could still be changed.
    if (disabled || state === 'disabled') return;
    setDisplay(displayFor(value));
    if (isOpen) setIsOpen(false);
    if (valueInputRef.current) {
      valueInputRef.current.value = value;
      // The hidden input listens with onInput, so this has to be an `input`
      // event. It used to dispatch one at an onChange listener -- which in
      // Preact is the native `change` event -- so picking an option moved the
      // box's own display and told the parent nothing.
      valueInputRef.current.dispatchEvent(new Event('input', { bubbles: true }));
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
  // the parent echoes it back through `value`. Keyed on the value and the
  // option list, which is everything displayFor reads.
  useEffect(
    () => {
      // eslint-disable-next-line @eslint-react/set-state-in-effect -- see above
      setDisplay(options.find((o) => o.value === value)?.display ?? value);
    },
    [value, options],
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
        {/* The field. Only this one is named, so a form submission carries
            one entry -- both inputs used to share elemName and send two. */}
        <input
          type="hidden"
          ref={valueInputRef}
          onInput={handleInput}
          name={elemName}
          value={value}
          disabled={state === 'disabled'}
        />
        {/* The box. Named after nothing, so it submits nothing, and operable
            from the keyboard: it used to open on click alone, with the
            options as unfocusable list items, so there was no way in. */}
        <input
          className={selectStyleFab(state)}
          {...(state !== 'disabled' && {
            onClick: () => !isOpen && setIsOpen(true),
            onKeyDown: (e: KeyboardEvent) => {
              if (e.key === 'Escape') { setIsOpen(false); return; }
              if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
                e.preventDefault();
                setIsOpen(true);
              }
            },
          })}
          readOnly={true}
          disabled={state === 'disabled'}
          value={display}
          id={elemName}
          role="combobox"
          aria-expanded={isOpen}
          aria-controls={`${elemName}-options`}
        />
        <div className="absolute inset-y-0 right-2 flex flex-col justify-center" {...(state !== 'disabled' && {onClick: () => !isOpen && setIsOpen(true)})}>
          <ArrowDown />
        </div>
        { isOpen &&
          <ul id={`${elemName}-options`} role="listbox" className="
            absolute z-50 flex w-full cursor-default flex-col gap-y-px
            rounded-sm border border-grey bg-white px-1 py-2 shadow-md
          ">
            {
              options.map(({value: optValue, option, disabled}) => <li
                className={`
                  min-h-[28px] rounded-sm px-1 py-[2px]
                  ${!disabled && `hover:bg-light-blue`}
                  ${disabled && 'cursor-not-allowed text-grey'}
                `}
                role="option"
                aria-selected={optValue === value}
                aria-disabled={disabled ?? false}
                tabIndex={disabled ? -1 : 0}
                onClick={(e) => {
                  e.stopPropagation();
                  handleOptionClick(optValue, disabled ?? false);
                }}
                onKeyDown={(e: KeyboardEvent) => {
                  if (e.key !== 'Enter' && e.key !== ' ') return;
                  e.preventDefault();
                  e.stopPropagation();
                  handleOptionClick(optValue, disabled ?? false);
                }}
                key={optValue}
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
      { label && <label for={elemName} className="text-sm">{label}</label> }
      { getSelectBody() }
      { description && <div className="text-sm">{description}</div> }
    </div>
  );
}
