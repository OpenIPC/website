import type { State } from '../types';

export type ElemNames =
  'MTD-device-name' |
  'flash-size' |
  'initial-offset' |
  'part0-name' |
  'part0-size' |
  'part0-start' |
  'part0-size-hex' |
  'part0-end' |
  'part1-name' |
  'part1-size' |
  'part1-start' |
  'part1-size-hex' |
  'part1-end' |
  'part2-name' |
  'part2-size' |
  'part2-start' |
  'part2-size-hex' |
  'part2-end' |
  'part3-name' |
  'part3-size' |
  'part3-start' |
  'part3-size-hex' |
  'part3-end' |
  'part4-name' |
  'part4-size' |
  'part4-start' |
  'part4-size-hex' |
  'part4-end' |
  'part5-name' |
  'part5-size' |
  'part5-start' |
  'part5-size-hex' |
  'part5-end' |
  'part6-name' |
  'part6-size' |
  'part6-start' |
  'part6-size-hex' |
  'part6-end' |
  'part7-name' |
  'part7-size' |
  'part7-start' |
  'part7-size-hex' |
  'part7-end';

export type DependancyValidableElemNames = Extract<ElemNames, 'initial-offset' | `part${number}-size`>;
export type SchemaElem = { value: string, state: State, error: string };
export type ValidationElem = { fn: (value: string) => boolean, preventInput?:boolean, error: string };
export type FormSchema = Record<ElemNames, SchemaElem>;
/** Sparse: the derived columns (start/end/size-hex) have no validators. */
export type FormValidationSchema = Partial<Record<ElemNames, ValidationElem[]>>;
