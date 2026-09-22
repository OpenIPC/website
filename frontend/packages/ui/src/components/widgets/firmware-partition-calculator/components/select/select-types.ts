import type { State } from '../../types';

export type SelectProps = {
  label: string,
  value: string,
  elemName: string,
  options: {
    value: string,
    option: string
    display: string,
    disabled?: boolean,
  }[],
  required?: boolean,
  onChange: (e: Event) => void,
  state: State,
  errorText?: string,
  open?: boolean,
}
