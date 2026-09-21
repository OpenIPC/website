import { FormSchema } from '../hooks/useCalc-types';
import type { SelectProps } from '../../../ui/form-elems/customSelect/select-types';

export const MTDDevNameOpts: SelectProps['options'] = [
  {
    value: '',
    option: '',
    display: '',
  },
  { 
    value: 'hi_sfc',
    option: 'hi_sfc (HiSilicon)', 
    display: 'hi_sfc',
  },
  {
    value: 'hinand',
    option: 'hinand',
    display: 'hinand',
  },
  {
    value: 'jz_sfc',
    option: 'jz_sfc',
    display: 'jz_sfc',
  },
  {
    value: 'nor-flash',
    option: 'nor-flash',
    display: 'nor-flash',
  },
  {
    value: 'NOR_FLASH',
    option: 'NOR_FLASH (SigmaStar)',
    display: 'NOR_FLASH',
  }, 
  {
    value: 'sfc',
    option: 'sfc',
    display: 'sfc',
  },
  {
    value: 'spi0.0',
    option: 'spi0.0',
    display: 'spi0.0',
  },
  {
    value: 'spi_flash',
    option: 'spi_flash',
    display: 'spi_flash',
  },
  {
    value: 'xm_sfc',
    option: 'xm_sfc',
    display: 'xm_sfc',
  },
];

export const flashSizeOpts: SelectProps['options'] = [
  {
    value: '4',
    option: '4',
    display: 'NOR 4',
  },
  {
    value: '8',
    option: '8',
    display: 'NOR 8',
  },
  {
    value: '16',
    option: '16',
    display: 'NOR 16',
  },
  {
   value: '32',
   option: '32',
   display: 'NOR 32',
  },
  {
    value: '128',
    option: '128',
    display: 'NAND 128',
  },
  {
    value: '256',
    option: '256',
    display: 'NAND 256',
  },
  {
    value: '512',
    option: '512',
    display: 'NAND 512'
  },
];

export const liteConfig: Pick<FormSchema, 'MTD-device-name' | 'flash-size' | 'initial-offset' | `part${0|1|2|3|4}-name` | `part${0|1|2|3|4}-size`>  = {
  'MTD-device-name': {
    value: '',
    state: 'default',
    error: '',
  },
  'flash-size': {
    value: '8',
    state: 'valid',
    error: '',
  },
  'initial-offset': {
    value: '0x0',
    state: 'valid',
    error: '',
  },
  'part0-name': {
    value: 'boot',
    state: 'valid',
    error: '',
  },
  'part0-size': {
    value: '256',
    state: 'valid',
    error: '',
  },
  'part1-name': {
    value: 'env',
    state: 'valid',
    error: '',
  },
  'part1-size': {
    value: '64',
    state: 'valid',
    error: '',
  },
  'part2-name': {
    value: 'kernel',
    state: 'valid',
    error: '',
  },
  'part2-size': {
    value: '2048',
    state: 'valid',
    error: '',
  },
  'part3-name': {
    value: 'rootfs',
    state: 'valid',
    error: '',
  },
  'part3-size': {
    value: '5120',
    state: 'valid',
    error: '',
  },
  'part4-name': {
    value: 'rootfs_data',
    state: 'valid',
    error: '',
  },
  'part4-size': {
    value: '704',
    state: 'valid',
    error: '',
  },
}
export const ultimateConfig: Pick<FormSchema, 'MTD-device-name' | 'flash-size' | 'initial-offset' | `part${0|1|2|3|4}-name` | `part${0|1|2|3|4}-size`>  = {
  'MTD-device-name': {
    value: '',
    state: 'default',
    error: '',
  },
  'flash-size': {
    value: '16',
    state: 'valid',
    error: '',
  },
  'initial-offset': {
    value: '0x0',
    state: 'valid',
    error: '',
  },
  'part0-name': {
    value: 'boot',
    state: 'valid',
    error: '',
  },
  'part0-size': {
    value: '256',
    state: 'valid',
    error: '',
  },
  'part1-name': {
    value: 'env',
    state: 'valid',
    error: '',
  },
  'part1-size': {
    value: '64',
    state: 'valid',
    error: '',
  },
  'part2-name': {
    value: 'kernel',
    state: 'valid',
    error: '',
  },
  'part2-size': {
    value: '3072',
    state: 'valid',
    error: '',
  },
  'part3-name': {
    value: 'rootfs',
    state: 'valid',
    error: '',
  },
  'part3-size': {
    value: '10240',
    state: 'valid',
    error: '',
  },
  'part4-name': {
    value: 'rootfs_data',
    state: 'valid',
    error: '',
  },
  'part4-size': {
    value: '2752',
    state: 'valid',
    error: '',
  },
}
