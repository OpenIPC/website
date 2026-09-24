import H1 from '../../ui/headers/h1';
import Input from './components/input';
import Output from './components/output';
import Select from './components/select';
import MainButton from '../../ui/buttons/main-button';
import PartitionMap from './components/part-map';
import PartitionString from './components/part-string/PartitionString';
import useCalc from './hooks/useCalc';
import { FwCalcFormSchema } from './calcFormSchema';
import { FwCalcFormValidationSchema } from './calcFormValidationSchema';
import { MTDDevNameOpts, flashSizeOpts } from './constants';
import { DEFAULT_FW_CALC_LABELS, type FwCalcLabels } from './types';
import { useEffect, useMemo, useRef } from 'preact/hooks';
import { debounce } from '../../../utils';

/** The eight rows, and the colour each one is outlined in. */
// Indices for the form state, which is keyed `part0-name` and always has
// been. The LABELS are 1-8: the Rails page this replaces numbers its eight
// rows from one, and a calculator that renames every partition is not the same
// page. See ROW_LABEL below.
const PARTITIONS = [0, 1, 2, 3, 4, 5, 6, 7] as const;

/** What the reader is shown, against the index the state is keyed by. */
const rowLabel = (index: number) => index + 1;

/** Ruby's `%{name}`, which is the syntax the catalogue strings are written in. */
function fill(template: string, vars: Record<string, string | number>): string {
  return template.replace(/%\{(\w+)\}/g, (whole, name: string) =>
    name in vars ? String(vars[name]) : whole,
  );
}

interface FirmwarePartitionCalculatorProps {
  /**
   * What the calculator calls things. Partial, so a consumer overrides the
   * words it has a translation for and keeps English for the rest -- which is
   * what an incomplete catalogue should look like, rather than a hole.
   */
  labels?: Partial<FwCalcLabels>;
}

export default function FirmwarePartitionCalculator({ labels }: FirmwarePartitionCalculatorProps) {
  const t: FwCalcLabels = { ...DEFAULT_FW_CALC_LABELS, ...labels };

  const {
    handleOnChange, handleRecalculateBtnClick, formElemsState,
    applyLiteConfig, applyUltimateConfig, partMap, freeSpace, partString
  } = useCalc(FwCalcFormSchema, FwCalcFormValidationSchema, { kb: t.kb, bytes: t.bytes });

  function handleInputChange(e: Event) {
    if (e.target instanceof HTMLInputElement) {
      handleOnChange(e);
    }
  }

  // One debounce for the life of the component, calling through a ref to
  // whatever the current handler is.
  //
  // It used to be keyed on handleOnChange, which useCalc rebuilds every
  // render -- so every render produced a new debounced function with its own
  // timer, and a new timer cannot clear the previous one's. Nothing was
  // debounced, and a name edit still in flight would later write its stale
  // copy of the form back over whatever had been changed since.
  const latestHandlerRef = useRef(handleInputChange);
  useEffect(() => {
    latestHandlerRef.current = handleInputChange;
  });

  const debouncedHandleInputChange = useMemo(() => {
    const [ fn ] = debounce((e: Event) => latestHandlerRef.current(e), 500);
    return fn;
  }, []);

  return (
    <>
      <div className="py-4">
        <H1 content={t.title} />
      </div>
      <div className="mb-6 flex flex-row gap-x-1">
        <MainButton size='s' caption={t.lite} clickHandler={applyLiteConfig} />
        <MainButton size='s' caption={t.ultimate} clickHandler={applyUltimateConfig} />
        <div className="ml-auto">
          <MainButton size='s' caption={t.recalculate} clickHandler={handleRecalculateBtnClick} />
        </div>
      </div>
      <div className="flex flex-col gap-y-2">
        <div className="
          flex flex-col gap-y-2
          md:flex-row md:gap-x-2
        ">
          <div className="md:w-[calc(20%-6px)]">
            <Select
              label={t.mtdName}
              elemName='MTD-device-name'
              options={MTDDevNameOpts}
              required={true}
              onChange={handleOnChange}
              value={formElemsState['MTD-device-name'].value}
              state={formElemsState['MTD-device-name'].state}
              errorText={formElemsState['MTD-device-name'].error}
              open={formElemsState['MTD-device-name'].state === 'error' ? true : false}
            />
          </div>
          <div className="md:w-[calc(20%-6px)]">
            <Select label={t.flashSize} elemName='flash-size' options={flashSizeOpts} required={true} onChange={handleOnChange} value={formElemsState['flash-size'].value} state={formElemsState['flash-size'].state} errorText={formElemsState['flash-size'].error} />
          </div>
          <div className="md:w-[calc(20%-6px)]">
            <Input elemName='initial-offset' label={t.initialOffset} onInput={handleOnChange} borderWidth='1px' borderColor='default' value={formElemsState['initial-offset'].value} state={formElemsState['initial-offset'].state} errorText={formElemsState['initial-offset'].error} dir="rtl" />
          </div>
        </div>
        {PARTITIONS.map((n) => (
          <div key={n} className="
            mt-4 flex flex-col gap-y-2
            md:mt-0 md:flex-row md:items-center md:gap-x-2
          ">
            <div className="md:w-[20%]">
              <Input
                elemName={`part${n}-name`}
                label={fill(t.partitionName, { number: rowLabel(n) })}
                borderWidth='4px'
                borderColor={`partition${n}`}
                value={formElemsState[`part${n}-name`].value}
                state={formElemsState[`part${n}-name`].state}
                onInput={debouncedHandleInputChange}
              />
            </div>
            <div className="md:w-[20%]">
              <Input
                elemName={`part${n}-size`}
                label={fill(t.partitionSize, { number: rowLabel(n) })}
                borderWidth='1px'
                borderColor='default'
                value={formElemsState[`part${n}-size`].value}
                onInput={handleInputChange}
                state={formElemsState[`part${n}-size`].state}
                errorText={formElemsState[`part${n}-size`].error}
              />
            </div>
            <div className="md:w-[20%]">
              <Output label={t.startAddress} data={formElemsState[`part${n}-start`].value} />
            </div>
            <div className="md:w-[20%]">
              <Output label={t.hexSize} data={formElemsState[`part${n}-size-hex`].value} />
            </div>
            <div className="md:w-[20%]">
              <Output label={t.endAddress} data={formElemsState[`part${n}-end`].value} />
            </div>
          </div>
        ))}
      </div>
      <div className="py-4">
        <PartitionMap slices={partMap} freeSpace={freeSpace} freeSpaceLabel={t.freeSpace} />
      </div>
      <div>
        <PartitionString partStrData={partString} />
      </div>
    </>
  );
}
