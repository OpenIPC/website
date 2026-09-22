import { FunctionComponent } from 'preact';

type ToggleButtonProps = {
  size: 'xs' | 's' | 'm' | 'l',
  checked?: boolean,
  disabled?: boolean,
  Icon?: FunctionComponent,
  /** Spoken name. An icon-only toggle announces nothing without it. */
  label?: string,
  changeHandler: (checked: boolean) => void,
}

export default ToggleButtonProps;
