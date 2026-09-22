export type SelectProps = {
  state: 'default' | 'valid' | 'error' | 'disabled',
  value: string,
  elemName: string,
  onChange: (e: Event) => void,
  options: {
    value: string,
    option: string
    display: string,
    disabled?: boolean,
  }[],
  open?: boolean,
  size?: 'sm' | 'md' | 'xl',
  label?: string,
  description?: string,
}
