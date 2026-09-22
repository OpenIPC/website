import { FunctionComponent } from 'preact';

type ToggleButtonProps = {
  size: 'xs' | 's' | 'm' | 'l',
  checked?: boolean,
  disabled?: boolean,
  Icon?: FunctionComponent,
  changeHandler: (checked: boolean) => void,
}

export default ToggleButtonProps;
