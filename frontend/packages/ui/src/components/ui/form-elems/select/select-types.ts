export type SelectProps = {
  elemName: string,
  label: string,
  state: 'default' | 'valid' | 'error' | 'disabled',
  onInput: (e: Event) => void,
  options: {
    value: string,
    disabled?: boolean,
  }[],
  required?: boolean,
  description?: string,
}
