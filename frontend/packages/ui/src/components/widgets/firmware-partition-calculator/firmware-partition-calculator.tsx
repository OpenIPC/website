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
import { useEffect, useMemo, useRef } from 'preact/hooks';
import { debounce } from '../../../utils';

export default function FirmwarePartitionCalculator() {
  const {
    handleOnChange, handleRecalculateBtnClick, formElemsState,
    applyLiteConfig, applyUltimateConfig, partMap, freeSpace, partString
  } = useCalc(FwCalcFormSchema, FwCalcFormValidationSchema);

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
        <H1 content="Firmware Partition Calculator" />
      </div>
      <div className="mb-6 flex flex-row gap-x-1">
        <MainButton size='s' caption="Lite" clickHandler={applyLiteConfig} />
        <MainButton size='s' caption="Ultimate" clickHandler={applyUltimateConfig} />
        <div className="ml-auto">
          <MainButton size='s' caption="Recalculate" clickHandler={handleRecalculateBtnClick} />
        </div>
      </div>
      <div className="flex flex-col gap-y-2">
        <div className="
          flex flex-col gap-y-2
          md:flex-row md:gap-x-2
        ">
          <div className="md:w-[calc(20%-6px)]">
            <Select
              label='MTD device name'
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
            <Select label='Flash size, MB' elemName='flash-size' options={flashSizeOpts} required={true} onChange={handleOnChange} value={formElemsState['flash-size'].value} state={formElemsState['flash-size'].state} errorText={formElemsState['flash-size'].error} />
          </div>
          <div className="md:w-[calc(20%-6px)]">
            <Input elemName='initial-offset' label='Initial offset, dec or hex, bytes' onInput={handleOnChange} borderWidth='1px' borderColor='default' value={formElemsState['initial-offset'].value} state={formElemsState['initial-offset'].state} errorText={formElemsState['initial-offset'].error} dir="rtl" />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part0-name' label='Partition 0 name' borderWidth='4px' borderColor='partition0' value={formElemsState['part0-name'].value} state={formElemsState['part0-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part0-size' label='Partition 0 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part0-size'].value} onInput={handleInputChange} state={formElemsState['part0-size'].state} errorText={formElemsState['part0-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part0-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part0-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address"  data={formElemsState['part0-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part1-name' label='Partition 1 name' borderWidth='4px' borderColor='partition1' value={formElemsState['part1-name'].value} state={formElemsState['part1-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part1-size' label='Partition 1 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part1-size'].value} onInput={handleInputChange} state={formElemsState['part1-size'].state} errorText={formElemsState['part1-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part1-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part1-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part1-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part2-name' label='Partition 2 name' borderWidth='4px' borderColor='partition2' value={formElemsState['part2-name'].value} state={formElemsState['part2-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part2-size' label='Partition 2 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part2-size'].value} onInput={handleInputChange} state={formElemsState['part2-size'].state} errorText={formElemsState['part2-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part2-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part2-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part2-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part3-name' label='Partition 3 name' borderWidth='4px' borderColor='partition3' value={formElemsState['part3-name'].value} state={formElemsState['part3-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part3-size' label='Partition 3 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part3-size'].value} onInput={handleInputChange} state={formElemsState['part3-size'].state} errorText={formElemsState['part3-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part3-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part3-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part3-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part4-name' label='Partition 4 name' borderWidth='4px' borderColor='partition4' value={formElemsState['part4-name'].value} state={formElemsState['part4-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part4-size' label='Partition 4 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part4-size'].value} onInput={handleInputChange} state={formElemsState['part4-size'].state} errorText={formElemsState['part4-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part4-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part4-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part4-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part5-name' label='Partition 5 name' borderWidth='4px' borderColor='partition5' value={formElemsState['part5-name'].value} state={formElemsState['part5-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part5-size' label='Partition 5 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part5-size'].value} onInput={handleInputChange} state={formElemsState['part5-size'].state} errorText={formElemsState['part5-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part5-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part5-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part5-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part6-name' label='Partition 6 name' borderWidth='4px' borderColor='partition6' value={formElemsState['part6-name'].value} state={formElemsState['part6-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part6-size' label='Partition 6 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part6-size'].value} onInput={handleInputChange} state={formElemsState['part6-size'].state} errorText={formElemsState['part6-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part6-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part6-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part6-end'].value} />
          </div>
        </div>
        <div className="
          mt-4 flex flex-col gap-y-2
          md:mt-0 md:flex-row md:items-center md:gap-x-2
        ">
          <div className="md:w-[20%]">
            <Input elemName='part7-name' label='Partition 7 name' borderWidth='4px' borderColor='partition7' value={formElemsState['part7-name'].value} state={formElemsState['part7-name'].state} onInput={debouncedHandleInputChange} />
          </div>
          <div className="md:w-[20%]">
            <Input elemName='part7-size' label='Partition 7 size, KB' borderWidth='1px' borderColor='default' value={formElemsState['part7-size'].value} onInput={handleInputChange} state={formElemsState['part7-size'].state} errorText={formElemsState['part7-size'].error} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Start address" data={formElemsState['part7-start'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="Hex size, bytes" data={formElemsState['part7-size-hex'].value} />
          </div>
          <div className="md:w-[20%]">
            <Output label="End address" data={formElemsState['part7-end'].value} />
          </div>
        </div>
      </div>
      <div className="py-4">
        <PartitionMap slices={partMap} freeSpace={freeSpace} />
      </div>
      <div>
        <PartitionString partStrData={partString} />
      </div>
    </>
  );
}
