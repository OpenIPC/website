import type { InputProps } from '../../components/ui/form-elems/input/Input-types';

export type FormSchema = Record<string, { value: string, state: InputProps.state, error: string }>;

export type FormValidationSchema = Record<string, { fn: (value: string) => boolean, error: string }[]>;
