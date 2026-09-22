import type { SelectProps } from './select-types';
import CustomSelect from '../../../../ui/form-elems/customSelect';

export default function Select({ elemName, value, label, options, required, onChange, state, errorText, open }: SelectProps) {

  return (
    <div className="
      flex w-full flex-col rounded-md border border-wallet-border bg-wallet-bg
    ">
      <p className="mt-0.5 ml-1 w-fit truncate text-sm text-dark-grey">{label}{required && <sup className="
        text-red
      "> *</sup>}</p>
      <div className="m-2 mb-0">
        <CustomSelect {...{elemName, value, state, options, onChange, open}} size='sm' />
      </div>
      <p className="min-h-5 pl-3 text-xs text-red">{state === 'error' && (errorText ?? '')}</p>
    </div>
  );
}
