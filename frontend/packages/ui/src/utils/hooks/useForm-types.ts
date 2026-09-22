import type { InputProps } from '../../components/ui/form-elems/input/Input-types';

// `InputProps.state` -- a namespace access on a type -- until these files
// stopped being .d.ts, where the error went unreported.
export type FormSchema = Record<string, {
  value: string,
  state: InputProps['state'],
  error: string,
}>;

export type FormValidationSchema = Record<string, {
  fn: (value: string) => boolean,
  error: string,
}[]>;
