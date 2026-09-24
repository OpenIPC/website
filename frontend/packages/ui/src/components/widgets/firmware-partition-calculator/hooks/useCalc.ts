import { useState } from 'preact/hooks';
import type { ElemNames, SchemaElem, FormSchema, ValidationElem, FormValidationSchema, DependancyValidableElemNames } from './useCalc-types';
import type { SliceData } from '../components/part-map/part-map-types';
import { liteConfig, ultimateConfig } from '../constants';
import { kiloBytesToBytes, megaBytesToBytes } from '../../../../utils/converters';

/**
 * The two units the free-space readout spells out (#160).
 *
 * Defaulted rather than required, so every existing caller and every test
 * keeps the English it was written against. The calculator's own labels are a
 * prop now, and "4096 KB" sitting under "Размер раздела 0, КБ" was the one
 * word left in the wrong language.
 */
export interface CalcUnits {
  kb: string;
  bytes: string;
}

const DEFAULT_UNITS: CalcUnits = { kb: 'KB', bytes: 'bytes' };

export default function useCalc(
  formSchema: FormSchema,
  formValidationSchema: FormValidationSchema,
  units: Partial<CalcUnits> = {},
) {
  const { kb, bytes } = { ...DEFAULT_UNITS, ...units };

  const [ formElemsState, setFormElemsState ] = useState(formSchema); 
  const [ partMap, setPartMap ] = useState<SliceData[]>([]);
  const [ freeSpace, setFreeSpace ] = useState(
    () => `${megaBytesToBytes(Number.parseInt(formSchema['flash-size'].value)) / 1024} ${kb}`,
  );
  const [ partString, setPartString ] = useState<string>('');

  /**
   * Blank the derived columns. They describe a layout that no longer exists
   * the moment any size or offset changes, and leaving them on screen next to
   * edited inputs is how somebody copies an address from the layout before.
   */
  function clearComputedColumns(state: FormSchema): FormSchema {
    let next = { ...state };
    for (let i = 0; i < 8; i++) {
      next = { ...next, ...{
        [`part${i}-start`]: { value: '', state: 'default', error: '' },
        [`part${i}-size-hex`]: { value: '', state: 'default', error: '' },
        [`part${i}-end`]: { value: '', state: 'default', error: '' },
      } };
    }
    return next;
  }

  function isValidElemName(name: string): name is ElemNames {
    return Object.keys(formSchema).includes(name);
  }

  function handleOnChange(e: Event) {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) {
      const { name, value } = e.target;
      if (isValidElemName(name)) {
        setFormElemsState({...formElemsState, [name]: getCurElemState(value, {...formElemsState[name]}, formValidationSchema[name])});
        const curFormElemsState = {...formElemsState, [name]: getCurElemState(value, {...formElemsState[name]}, formValidationSchema[name])};
        const curFormElemsStateDepCheck = clearComputedColumns(
          validateDependencies(curFormElemsState),
        );
        setFormElemsState(curFormElemsStateDepCheck);
        setPartMap(getPartMapSlices(curFormElemsStateDepCheck));
        getFreeSpace(curFormElemsStateDepCheck);
        setPartString('');
      }
    }
  }

  function getCurElemState(value: string, curElemState: SchemaElem, elemValidators?: ValidationElem[]): SchemaElem {
    for (const { fn, error, preventInput } of elemValidators ?? []) {
      if (fn(value)) continue;
      return preventInput
        ? curElemState 
        : { value, state: 'error', error };
    }
    
    return value === ''
      ? { value, state: 'default', error: '' }
      : { value, state: 'valid', error: '' };
  }

  function validateDependencies(curFormElemsState: FormSchema ): FormSchema {
    const elemNames = Object.keys(formElemsState).filter(elemName => /^part[0-7]-size$/.test(elemName) || elemName === 'initial-offset') as DependancyValidableElemNames[];
    const curFormElemsStateDepCheck: FormSchema = { ...curFormElemsState };
    const size = curFormElemsState['flash-size'].value;
    const offset = curFormElemsState['initial-offset'].value;
    const part0Size = curFormElemsState['part0-size'].value;
    const part1Size = curFormElemsState['part1-size'].value;
    const part2Size = curFormElemsState['part2-size'].value;
    const part3Size = curFormElemsState['part3-size'].value;
    const part4Size = curFormElemsState['part4-size'].value;
    const part5Size = curFormElemsState['part5-size'].value;
    const part6Size = curFormElemsState['part6-size'].value;
    const part7Size = curFormElemsState['part7-size'].value;

    const isElemValid = (name: DependancyValidableElemNames, value: string) => {
        const res = getCurElemState(value, curFormElemsStateDepCheck[name], formValidationSchema[name]);
        return res.state === 'error' ? false : true;
    }

    const isPrevElemsValid = (curElem: DependancyValidableElemNames) => {
      const prevElems = elemNames.slice(0, elemNames.findIndex(elem => curElem === elem));
      return !prevElems.some(el => curFormElemsStateDepCheck[el].state === 'error');
    }

    const validators: Record<DependancyValidableElemNames, () => void> = {
      'initial-offset': () => {
        if (!isElemValid('initial-offset', offset)) return; 
        if (megaBytesToBytes(Number.parseInt(size)) <= Number.parseInt(offset)) {
          curFormElemsStateDepCheck['initial-offset'] = { value: offset, state: 'error', error: 'Offset must be less than flash size' };
        } else {
          curFormElemsStateDepCheck['initial-offset'] = { value: offset, state: 'valid', error: '' };
        }
      },
      'part0-size': () => {
        if (!part0Size || !isElemValid('part0-size', part0Size)) return;
        if (!isElemValid('initial-offset', offset)) {
          curFormElemsStateDepCheck['part0-size'] = { value: part0Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (
         kiloBytesToBytes(Number.parseInt(part0Size)) > (megaBytesToBytes(Number.parseInt(size)) - Number.parseInt(offset))
        ) {
          curFormElemsStateDepCheck['part0-size'] = { value: part0Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part0-size'] = { value: part0Size, state: 'valid', error: '' };
        }
      },
      'part1-size': () => {
        if (!part1Size || !isElemValid('part1-size', part1Size)) return;
        if (!isPrevElemsValid('part1-size')) {
          curFormElemsStateDepCheck['part1-size'] = { value: part1Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part0Size) {
          curFormElemsStateDepCheck['part1-size'] = { value: part1Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
         kiloBytesToBytes(Number.parseInt(part1Size)) > (megaBytesToBytes(Number.parseInt(size)) - Number.parseInt(offset) - kiloBytesToBytes(Number.parseInt(part0Size)))
        ) {
          curFormElemsStateDepCheck['part1-size'] = { value: part1Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part1-size'] = { value: part1Size, state: 'valid', error: '' };
        }
      },
      'part2-size': () => {
        if (!part2Size || !isElemValid('part2-size', part2Size)) return;
        if (!isPrevElemsValid('part2-size')) {
          curFormElemsStateDepCheck['part2-size'] = { value: part2Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part1Size) {
          curFormElemsStateDepCheck['part2-size'] = { value: part2Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
          kiloBytesToBytes(Number.parseInt(part2Size)) > (
            megaBytesToBytes(Number.parseInt(size)) -
            Number.parseInt(offset) -
            kiloBytesToBytes(Number.parseInt(part0Size)) -
            kiloBytesToBytes(Number.parseInt(part1Size))
          )
        ) {
          curFormElemsStateDepCheck['part2-size'] = { value: part2Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part2-size'] = { value: part2Size, state: 'valid', error: '' };
        }
      },
      'part3-size': () => {
        if (!part3Size || !isElemValid('part3-size', part3Size)) return;
        if (!isPrevElemsValid('part3-size')) {
          curFormElemsStateDepCheck['part3-size'] = { value: part3Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part2Size) {
          curFormElemsStateDepCheck['part3-size'] = { value: part3Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
          kiloBytesToBytes(Number.parseInt(part3Size)) > (
            megaBytesToBytes(Number.parseInt(size)) -
            Number.parseInt(offset) -
            kiloBytesToBytes(Number.parseInt(part0Size)) -
            kiloBytesToBytes(Number.parseInt(part1Size)) -
            kiloBytesToBytes(Number.parseInt(part2Size))
          )
        ) {
          curFormElemsStateDepCheck['part3-size'] = { value: part3Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part3-size'] = { value: part3Size, state: 'valid', error: '' };
        }
      },
      'part4-size': () => {
        if (!part4Size || !isElemValid('part4-size', part4Size)) return;
        if (!isPrevElemsValid('part4-size')) {
          curFormElemsStateDepCheck['part4-size'] = { value: part4Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part3Size) {
          curFormElemsStateDepCheck['part4-size'] = { value: part4Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
          kiloBytesToBytes(Number.parseInt(part4Size)) > (
            megaBytesToBytes(Number.parseInt(size)) -
            Number.parseInt(offset) -
            kiloBytesToBytes(Number.parseInt(part0Size)) -
            kiloBytesToBytes(Number.parseInt(part1Size)) -
            kiloBytesToBytes(Number.parseInt(part2Size)) -
            kiloBytesToBytes(Number.parseInt(part3Size))
          )
        ) {
          curFormElemsStateDepCheck['part4-size'] = { value: part4Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part4-size'] = { value: part4Size, state: 'valid', error: '' };
        }
      },
      'part5-size': () => {
        if (!part5Size || !isElemValid('part5-size', part5Size)) return;
        if (!isPrevElemsValid('part5-size')) {
          curFormElemsStateDepCheck['part5-size'] = { value: part5Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part4Size) {
          curFormElemsStateDepCheck['part5-size'] = { value: part5Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
          kiloBytesToBytes(Number.parseInt(part5Size)) > (
            megaBytesToBytes(Number.parseInt(size)) -
            Number.parseInt(offset) -
            kiloBytesToBytes(Number.parseInt(part0Size)) -
            kiloBytesToBytes(Number.parseInt(part1Size)) -
            kiloBytesToBytes(Number.parseInt(part2Size)) -
            kiloBytesToBytes(Number.parseInt(part3Size)) -
            kiloBytesToBytes(Number.parseInt(part4Size))
          )
        ) {
          curFormElemsStateDepCheck['part5-size'] = { value: part5Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part5-size'] = { value: part5Size, state: 'valid', error: '' };
        }
      },
      'part6-size': () => {
        if (!part6Size || !isElemValid('part6-size', part6Size)) return;
        if (!isPrevElemsValid('part6-size')) {
          curFormElemsStateDepCheck['part6-size'] = { value: part6Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part5Size) {
          curFormElemsStateDepCheck['part6-size'] = { value: part6Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
          kiloBytesToBytes(Number.parseInt(part6Size)) > (
            megaBytesToBytes(Number.parseInt(size)) -
            Number.parseInt(offset) -
            kiloBytesToBytes(Number.parseInt(part0Size)) -
            kiloBytesToBytes(Number.parseInt(part1Size)) -
            kiloBytesToBytes(Number.parseInt(part2Size)) -
            kiloBytesToBytes(Number.parseInt(part3Size)) -
            kiloBytesToBytes(Number.parseInt(part4Size)) -
            kiloBytesToBytes(Number.parseInt(part5Size))
          )
        ) {
          curFormElemsStateDepCheck['part6-size'] = { value: part6Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part6-size'] = { value: part6Size, state: 'valid', error: '' };
        }
      },
      'part7-size': () => {
        if (!part7Size || !isElemValid('part7-size', part7Size)) return;
        if (!isPrevElemsValid('part7-size')) {
          curFormElemsStateDepCheck['part7-size'] = { value: part7Size, state: 'error', error: 'Previous fields are incorrect' };
          return;
        }
        if (!part6Size) {
          curFormElemsStateDepCheck['part7-size'] = { value: part7Size, state: 'error', error: 'Fill previous field' };
          return;
        }
        if (
          kiloBytesToBytes(Number.parseInt(part7Size)) > (
            megaBytesToBytes(Number.parseInt(size)) -
            Number.parseInt(offset) -
            kiloBytesToBytes(Number.parseInt(part0Size)) -
            kiloBytesToBytes(Number.parseInt(part1Size)) -
            kiloBytesToBytes(Number.parseInt(part2Size)) -
            kiloBytesToBytes(Number.parseInt(part3Size)) -
            kiloBytesToBytes(Number.parseInt(part4Size)) -
            kiloBytesToBytes(Number.parseInt(part5Size)) -
            kiloBytesToBytes(Number.parseInt(part6Size))
          )
        ) {
          curFormElemsStateDepCheck['part7-size'] = { value: part7Size, state: 'error', error: 'Out of free space' };
        } else {
          curFormElemsStateDepCheck['part7-size'] = { value: part7Size, state: 'valid', error: '' };
        }
      },
    }

    for (const elemName of elemNames) {
      validators[elemName]();
    }
    return curFormElemsStateDepCheck;
  }

  function applyPredefinedConfig(configName: 'lite' | 'ultimate') {
    const configs = {
      lite: liteConfig,
      ultimate: ultimateConfig,
    }

    const elemNames = Object.keys(formElemsState).filter(elemName => /^part[0-7]-size$/.test(elemName)) as DependancyValidableElemNames[];
    const preset = configs[configName];
    // A preset is a whole layout, not an overlay. Clear every partition first,
    // or the rows a longer layout left behind stay in the free-space sum and
    // in the exported line, past the end of the preset's flash.
    let tempFormElemsState = { ...formElemsState };
    for (let i = 0; i < elemNames.length; i++) {
      tempFormElemsState = { ...tempFormElemsState, ...{
        [`part${i}-name`]: { value: '', state: 'default', error: '' },
        [`part${i}-size`]: { value: '', state: 'default', error: '' },
      } };
    }
    tempFormElemsState = { ...tempFormElemsState, ...preset };
    for (let i = 0; i < elemNames.length; i++) {
      tempFormElemsState = { ...tempFormElemsState, ...{
          [`part${i}-start`]: {
            value: '',
            state: 'default',
            error: '',
          },
          [`part${i}-size-hex`]: {
            value: '',
            state: 'default',
            error: '',
          },
          [`part${i}-end`]: {
            value: '',
            state: 'default',
            error: '',
          },
      },
      };
    }
    setFormElemsState(tempFormElemsState);
    setPartMap(getPartMapSlices(tempFormElemsState));
    getFreeSpace(tempFormElemsState);
    setPartString('');
  }

  function applyLiteConfig() {
    applyPredefinedConfig('lite');
  }

  function applyUltimateConfig() {
    applyPredefinedConfig('ultimate');
  }
  
  /**
   * The initial-offset field accepts either notation -- isDecOrHexNumber lets
   * `4096` and `0x1000` both through, and getFreeSpace() reads it with
   * parseInt's own prefix detection. recalculate() used to force radix 16, so
   * a decimal 4096 was laid out at 0x4096 and every address after it followed
   * that wrong start.
   */
  const parseOffset = (value: string): number => {
    const n = /^0[xX]/.test(value) ? Number.parseInt(value, 16) : Number.parseInt(value, 10);
    return Number.isNaN(n) ? 0 : n;
  };

  const getAddresses = (sizeKb: number, start: number): {start: string, size: string, end: string} => {
    const sizeBytes = kiloBytesToBytes(sizeKb);
    return {
      start: `0x${start.toString(16)}`,
      size: `0x${sizeBytes.toString(16)}`,
      end: `0x${(start+sizeBytes-1).toString(16)}`,
    };
  }

  function recalculate() {
    const elemNames = Object.keys(formElemsState).filter(elemName => /^part[0-7]-size$/.test(elemName)) as DependancyValidableElemNames[];
    if (formElemsState['MTD-device-name'].value === '') {
      setFormElemsState({...formElemsState, 'MTD-device-name': { value: '', state: 'error', error: 'Required field' }});
      return;
    }

    if (elemNames.some(el => formElemsState[el].state === 'error')) return;
    
    let tempFormElemsState = { ...formElemsState };
    for (let i = 0; i < elemNames.length; i++) {
      tempFormElemsState = {
        ...tempFormElemsState, 
        ...{
          [`part${i}-start`]: {
            value: '',
            state: 'default',
            error: '',
          },
          [`part${i}-size-hex`]: {
            value: '',
            state: 'default',
            error: '',
          },
          [`part${i}-end`]: {
            value: '',
            state: 'default',
            error: '',
          },
        },
      };
    }

    for (let i = 0; i < elemNames.length; i++) {
      if (tempFormElemsState[elemNames[i]].state === 'valid') {
        const addresses = getAddresses(Number.parseInt(tempFormElemsState[elemNames[i]].value), i > 0 ? Number.parseInt(tempFormElemsState[(`part${i-1}-end`) as unknown as DependancyValidableElemNames].value, 16) + 1 : parseOffset(tempFormElemsState['initial-offset'].value));
        tempFormElemsState = {
          ...tempFormElemsState, 
          ...{
            [`part${i}-start`]: {
              value: addresses.start,
              state: 'default',
              error: '',
            },
            [`part${i}-size-hex`]: {
              value: addresses.size,
              state: 'default',
              error: '',
            },
            [`part${i}-end`]: {
              value: addresses.end,
              state: 'default',
              error: '',
            },
          },
        };
      } else {
        if (tempFormElemsState[elemNames[i]].state === 'error') return;
        break;
      }
    }
    setFormElemsState(tempFormElemsState);
    setPartString(getPartString(tempFormElemsState));
  }

  /**
   * The bar under the form. Its grey remainder is labelled "free space", so
   * anything occupied has to be drawn -- including the initial offset, which
   * getFreeSpace() subtracts but this used to ignore, leaving reserved flash
   * looking like room for another partition.
   *
   * Widths come from cumulative boundaries rather than from rounding each
   * region on its own. Rounded independently, the Lite preset's 256, 64,
   * 2048, 5120 and 704 KB came to 3 + 1 + 25 + 63 + 9 = 101% of a chip they
   * exactly fill, and the map clips at overflow-hidden -- so a layout that
   * fitted perfectly lost the end of its last partition. Rounding the
   * running total instead makes the parts sum to the whole by construction.
   *
   * A region under half a percent therefore rounds to zero width. The map
   * gives every slice a one-pixel minimum in CSS, which shows it without
   * putting the arithmetic back out of true.
   */
  function getPartMapSlices(formState: FormSchema): SliceData[] {
    const total = megaBytesToBytes(Number.parseInt(formState['flash-size'].value));
    if (!total) return [];

    const regions: { bytes: number, color: SliceData['color'] }[] = [];

    const offsetState = formState['initial-offset'];
    const offset = offsetState.state === 'valid' ? parseOffset(offsetState.value) : 0;
    if (offset > 0) regions.push({ bytes: offset, color: 'reserved' });

    for (let i = 0; i < 8; i++) {
      const part = formState[`part${i}-size` as ElemNames];
      if (!part.value || part.state !== 'valid') continue;
      regions.push({
        bytes: kiloBytesToBytes(Number.parseInt(part.value)),
        color: `partition${i}` as SliceData['color'],
      });
    }

    const slices: SliceData[] = [];
    let consumed = 0;
    let drawn = 0;
    for (const { bytes, color } of regions) {
      consumed += bytes;
      const boundary = Math.min(100, Math.round(consumed / total * 100));
      slices.push({ width: Math.max(0, boundary - drawn), color });
      drawn = boundary;
    }

    return slices;
  }

  function getFreeSpace(formState: FormSchema) {
    const countableFields = Object.keys(formState).filter(key => /^part\d-size$/i.test(key));
    const flashSize = megaBytesToBytes(Number.parseInt(formState['flash-size'].value));
    let freeSpace = flashSize;
    if (formState['initial-offset'].state === 'valid') {
      freeSpace = flashSize - Number.parseInt(formState['initial-offset'].value);
    } else {
      setFreeSpace(freeSpace % 1024 === 0 ? `${freeSpace / 1024} ${kb}` : `${Math.floor(freeSpace / 1024)} ${kb}, ${freeSpace % 1024} ${bytes}`);
      return;
    }
    for (const field of countableFields) {
      const { value, state } = formState[field as ElemNames];
      if (state === 'valid') {
        freeSpace = freeSpace - kiloBytesToBytes(Number.parseInt(value));
      } else {
        setFreeSpace(freeSpace % 1024 === 0 ? `${freeSpace / 1024} ${kb}` : `${Math.floor(freeSpace / 1024)} ${kb}, ${freeSpace % 1024} ${bytes}`);
        break;
      }
    }
    setFreeSpace(freeSpace % 1024 === 0 ? `${freeSpace / 1024} ${kb}` : `${Math.floor(freeSpace / 1024)} ${kb}, ${freeSpace % 1024} ${bytes}`);
  }

  /**
   * The mtdparts line somebody pastes into a bootloader.
   *
   * Every partition carries its own `@<start>`, which mtdparts syntax allows
   * and which makes the line independent of what precedes it. That matters
   * because a sized partition with no name cannot be written -- `256k()` is
   * not a definition -- and it is therefore skipped, leaving a gap. With
   * offsets implied by position, skipping one shifted every later partition
   * earlier than the addresses on screen said, and the paste would have
   * landed in the wrong place. With each start stated, a gap is just a gap.
   *
   * It also used to drop the initial offset entirely, so a layout starting
   * at 0x40000 exported as one starting at zero.
   */
  function getPartString(formState: FormSchema) {
    const parts: string[] = [];
    for (let i = 0; i < 8; i++) {
      const size = formState[`part${i}-size` as ElemNames];
      const name = formState[`part${i}-name` as ElemNames].value;
      const start = formState[`part${i}-start` as ElemNames].value;
      if (size.state !== 'valid' || size.value === '' || name === '' || !start) continue;
      parts.push(`${size.value}k@${start}(${name})`);
    }
    return parts.length === 0
      ? ''
      : `${formState['MTD-device-name'].value}:${parts.join(',')}`;
  }

  function handleRecalculateBtnClick() {
    recalculate();
  }

  return { handleOnChange, handleRecalculateBtnClick, formElemsState, applyLiteConfig, applyUltimateConfig, partMap, freeSpace, partString };
}
