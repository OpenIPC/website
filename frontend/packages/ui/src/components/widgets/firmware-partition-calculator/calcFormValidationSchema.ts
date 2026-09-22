import type { FormValidationSchema, ValidationElem } from './hooks/useCalc-types';
import { isDecOrHexNumber, isNonEmpty, isDigitsOnly } from '../../../utils/validators';

/**
 * A partition name goes straight into `<size>k(<name>)`, where a comma ends
 * the definition and the parentheses delimit the name. Upstream accepted
 * every name, so `root,fs` exported a line the bootloader reads as two
 * partitions, the second of them nonsense. Whitespace goes too: mtdparts is
 * one unquoted argument.
 */
const partitionName: ValidationElem[] = [
  {
    fn: (val: string) => !/[,()\s]/.test(val),
    preventInput: true,
    error: '',
  },
];

export const FwCalcFormValidationSchema: FormValidationSchema = {
  'MTD-device-name': [
    {
      fn: isNonEmpty,
      error: 'Required field',
    },
  ],
  'flash-size': [
    {
      fn: isNonEmpty,
      error: 'Required field',
    },
  ],
  'initial-offset': [
    {
      fn: isNonEmpty,
      error: 'Required field',
    },
    {
      fn: isDecOrHexNumber,
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => {
        if (val === '0') return true;
        if (val === '0X' || val === '0x') return false;
        return isDecOrHexNumber(val);
      },
      error: 'Invalid hexademical number',
    },
  ],
  'part0-name': partitionName,
  'part0-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part1-name': partitionName,
  'part1-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part2-name': partitionName,
  'part2-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part3-name': partitionName,
  'part3-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part4-name': partitionName,
  'part4-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part5-name': partitionName,
  'part5-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part6-name': partitionName,
  'part6-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
  'part7-name': partitionName,
  'part7-size': [
    {
      fn: (val) => isDigitsOnly(val) || val === '',
      preventInput: true,
      error: '',
    },
    {
      fn: (val) => !val.length ? true : Number.parseInt(val) > 0,
      error: 'Size must be greater than zero',
    },
  ],
}
