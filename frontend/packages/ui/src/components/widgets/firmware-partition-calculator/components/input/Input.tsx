import type { InputProps } from './input-types';

export default function Input({ elemName, label, onInput, borderWidth, borderColor, dir, required, value, state, errorText }: InputProps) {

  const inputBorderColor: Record<InputProps['state'], string> = {
    default: '',
    valid: 'border-green',
    error: 'border-red',
    disabled: '',
  }


  const borderWidthClasses: Record<InputProps['borderWidth'], string> = {
    '1px': 'border',
    '4px': 'border-4',
  }

  const borderColorClasses: Record<InputProps['borderColor'], string> = {
    'default':  'border-wallet-border',
    'partition0': 'border-partition0',
    'partition1': 'border-partition1',
    'partition2': 'border-partition2',
    'partition3': 'border-partition3',
    'partition4': 'border-partition4',
    'partition5': 'border-partition5',
    'partition6': 'border-partition6',
    'partition7': 'border-partition7',
  }

  return (
    <div className={`
      border
      ${borderWidthClasses[borderWidth]}
      ${borderColorClasses[borderColor]}
      flex flex-col rounded-md bg-wallet-bg
      has-focus:outline-4 has-focus:outline-stages-border
    `}>
      <label for={elemName} className="
        mt-0.5 ml-1 truncate text-sm text-dark-grey
      ">{label}{required && <sup className="text-red"> *</sup>}</label>
      <input name={elemName} id={elemName} {...{dir, value, onInput}} className={`
        m-2 mb-0 h-7 w-[94%] rounded-sm border p-1 font-mono text-xl
        focus:outline-none
        ${inputBorderColor[state]}
      `} />
      <p className="min-h-5 pl-3 text-xs text-red">{state === 'error' && (errorText ?? '')}</p>
    </div>
  );
}
