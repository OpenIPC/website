import { FunctionComponent } from 'preact';

export type InputProps = {
  elemName: string,
  type: string,
  label: string,
  state: 'default' | 'valid' | 'error' | 'disabled',
  onInput: (e: Event) => void,
  required?: boolean,
  value?: string,
  placeholder?: string,
  Icon?: FunctionComponent,
  iconClickHandler?: (elem: HTMLInputElement, e: Event) => void,
  /** Spoken name for the icon action, when there is one. */
  iconLabel?: string,
  iconPlace?: 'left' | 'right',
  iconTooltip?: string,
  errorText?: string,
  description?: string,
}
