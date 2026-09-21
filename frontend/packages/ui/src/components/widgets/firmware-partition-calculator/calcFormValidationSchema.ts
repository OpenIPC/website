import type { FormValidationSchema } from './hooks/useCalc-types';
import { isDecOrHexNumber, isNonEmpty, isDigitsOnly } from '../../../utils/validators';

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
  'part0-name': [
    { 
      fn: () => true,
      error: '',
    },
  ],
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
  'part1-name': [
    { 
      fn: (_) => true,
      error: '',
    },
  ],
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
  'part2-name': [
    { 
      fn: (_) => true,
      error: '',
    },
  ],
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
  'part3-name': [
    { 
      fn: (_) => true,
      error: '',
    },
  ],
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
  'part4-name': [
    { 
      fn: (_) => true,
      error: '',
    },
  ],
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
  'part5-name': [
    { 
      fn: (_) => true,
      error: '',
    },
  ],
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
  'part6-name': [
    { 
      fn: () => true,
      error: '',
    },
  ],
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
  'part7-name': [
    { 
      fn: () => true,
      error: '',
    },
  ],
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
