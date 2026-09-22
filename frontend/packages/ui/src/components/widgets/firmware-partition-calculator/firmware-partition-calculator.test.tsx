/**
 * The calculator's arithmetic, locked.
 *
 * Its output is an `mtdparts` line a reader pastes into a bootloader, so a
 * wrong offset here is a bricked camera rather than a layout bug. The
 * expected addresses below are computed by hand from the presets and checked
 * against the component -- not snapshotted out of it.
 */
import { expect, test, describe, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/preact';
import FirmwarePartitionCalculator from './firmware-partition-calculator';

function field(name: string) {
  return document.querySelector(`input[name="${name}"]`) as HTMLInputElement;
}

function click(caption: string) {
  fireEvent.click(screen.getAllByText(caption)[0]);
}

function setMtdDeviceName(value: string) {
  // CustomSelect renders a visible box and a hidden value input, both named
  // after the field; the handler is the same for either.
  for (const input of document.querySelectorAll('input[name="MTD-device-name"]')) {
    fireEvent.input(input, { target: { value } });
  }
}

/** Start, hex size and end of every computed partition, in document order. */
function addresses() {
  return [...document.querySelectorAll('p[dir="rtl"]')]
    .map(p => p.textContent ?? '')
    .filter(Boolean);
}

function mtdparts() {
  return [...document.querySelectorAll('p')]
    .map(p => p.textContent ?? '')
    .find(t => /\dk@0x[0-9a-f]+\(/.test(t)) ?? '';
}

function freeSpace() {
  return document.querySelector('span')?.textContent ?? '';
}

describe('the Lite preset: 8 MB as 256 + 64 + 2048 + 5120 + 704 KB', () => {
  test('fills the chip exactly', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');

    expect(field('flash-size').value).toBe('8');
    expect(field('part0-name').value).toBe('boot');
    expect(field('part1-name').value).toBe('env');
    expect(field('part2-name').value).toBe('kernel');
    expect(field('part3-name').value).toBe('rootfs');
    expect(field('part4-name').value).toBe('rootfs_data');
    // 256 + 64 + 2048 + 5120 + 704 = 8192 KB = 8 MB.
    expect(freeSpace()).toBe('Free space: 0 KB');
  });

  test('lays the partitions end to end from the initial offset', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    setMtdDeviceName('hi_sfc');
    click('Recalculate');

    expect(addresses()).toEqual([
      '0x0',      '0x40000',  '0x3ffff',    // boot        256 KB
      '0x40000',  '0x10000',  '0x4ffff',    // env          64 KB
      '0x50000',  '0x200000', '0x24ffff',   // kernel     2048 KB
      '0x250000', '0x500000', '0x74ffff',   // rootfs     5120 KB
      '0x750000', '0xb0000',  '0x7fffff',   // rootfs_data 704 KB
    ]);
    // The last byte of the last partition is the last byte of an 8 MB chip.
    expect(Number.parseInt('0x7fffff', 16) + 1).toBe(8 * 1024 * 1024);
  });

  test('writes the mtdparts line that gets pasted into the bootloader', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    setMtdDeviceName('hi_sfc');
    click('Recalculate');

    // Every partition states its own start. See getPartString for why: a
    // partition that cannot be written leaves a gap, and offsets implied by
    // position would silently close it.
    expect(mtdparts()).toBe(
      'hi_sfc:'
      + '256k@0x0(boot),'
      + '64k@0x40000(env),'
      + '2048k@0x50000(kernel),'
      + '5120k@0x250000(rootfs),'
      + '704k@0x750000(rootfs_data)',
    );
  });
});

describe('the Ultimate preset', () => {
  test('is a 16 MB layout and also fills the chip', () => {
    render(<FirmwarePartitionCalculator />);
    click('Ultimate');

    expect(field('flash-size').value).toBe('16');
    expect(freeSpace()).toBe('Free space: 0 KB');
  });
});

describe('refusals', () => {
  test('Recalculate does nothing without an MTD device name', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    click('Recalculate');

    expect(addresses()).toEqual([]);
    expect(screen.getAllByText('Required field').length).toBeGreaterThan(0);
  });

  test('a partition size of zero is an error, not a zero-length partition', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    fireEvent.input(field('part0-size'), { target: { value: '0' } });

    expect(screen.getAllByText('Size must be greater than zero').length).toBeGreaterThan(0);
  });

  test('a non-numeric partition size is not accepted at all', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    fireEvent.input(field('part0-size'), { target: { value: '12ab' } });

    // preventInput: the field keeps its previous value rather than showing an
    // error, so nothing downstream ever sees a size that is not a number.
    expect(field('part0-size').value).toBe('256');
  });

  test('an initial offset that is not hex is refused', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    fireEvent.input(field('initial-offset'), { target: { value: '0x' } });

    expect(screen.getAllByText('Invalid hexademical number').length).toBeGreaterThan(0);
  });
});

describe('findings from the review of #260', () => {
  /**
   * The Lite preset fills its 8 MB exactly, so any initial offset overflows
   * it and recalculate() correctly refuses. Shrink a partition by the offset
   * first, which is what a real layout with a reserved head looks like.
   */
  function litePlusRoom(freeKb: number) {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    fireEvent.input(field('part3-size'), { target: { value: String(5120 - freeKb) } });
  }

  test('a decimal initial offset is decimal, not hex', () => {
    litePlusRoom(4);                       // 4 KB = 4096 bytes
    fireEvent.input(field('initial-offset'), { target: { value: '4096' } });
    setMtdDeviceName('hi_sfc');
    click('Recalculate');

    // 4096 decimal is 0x1000. Read as hex it would be 0x4096, and every
    // address after it would inherit that wrong start.
    expect(addresses()[0]).toBe('0x1000');
  });

  test('a hex initial offset still works', () => {
    litePlusRoom(4);
    fireEvent.input(field('initial-offset'), { target: { value: '0x1000' } });
    setMtdDeviceName('hi_sfc');
    click('Recalculate');

    expect(addresses()[0]).toBe('0x1000');
  });

  test('the exported line carries the offset it starts at', () => {
    litePlusRoom(256);                     // 256 KB = 0x40000
    fireEvent.input(field('initial-offset'), { target: { value: '0x40000' } });
    setMtdDeviceName('hi_sfc');
    click('Recalculate');

    // Without the @, pasting this writes the first partition over whatever
    // the reserved region below 0x40000 holds.
    expect(mtdparts()).toContain('256k@0x40000(boot)');
    expect(mtdparts()).toContain('64k@0x80000(env)');
    expect(addresses()[0]).toBe('0x40000');
  });

  test('a partition name cannot contain an mtdparts delimiter', async () => {
    vi.useFakeTimers();
    try {
      render(<FirmwarePartitionCalculator />);
      click('Lite');
      // The name fields are debounced by 500ms, so the rejection lands after
      // the timer -- and Preact re-renders on a microtask after that, which
      // is why this advances asynchronously.
      fireEvent.input(field('part0-name'), { target: { value: 'root,fs' } });
      await vi.advanceTimersByTimeAsync(600);

      expect(field('part0-name').value).toBe('boot');
    } finally {
      vi.useRealTimers();
    }
  });

  test('a preset replaces the layout rather than merging into it', () => {
    render(<FirmwarePartitionCalculator />);
    // A sixth partition, beyond anything the presets define.
    fireEvent.input(field('part5-size'), { target: { value: '512' } });
    click('Lite');

    expect(field('part5-size').value).toBe('');
    // 8 MB exactly, so the leftover row is not still eating into it.
    expect(freeSpace()).toBe('Free space: 0 KB');
  });

  test('editing a size clears the addresses it invalidated', () => {
    render(<FirmwarePartitionCalculator />);
    click('Lite');
    setMtdDeviceName('hi_sfc');
    click('Recalculate');
    expect(addresses().length).toBeGreaterThan(0);

    fireEvent.input(field('part2-size'), { target: { value: '1024' } });

    // Every start/end column described the layout before the edit.
    expect(addresses()).toEqual([]);
  });

  test('a gap left by an unnamed partition does not move the ones after it', async () => {
    vi.useFakeTimers();
    try {
      render(<FirmwarePartitionCalculator />);
      click('Lite');
      // Blank the name of the middle partition. Its 2048 KB still occupy the
      // layout -- the addresses on screen say so -- but it cannot be written
      // into the line. Skipping it used to pull rootfs back by 2048 KB,
      // because each partition's offset was implied by the one before it.
      fireEvent.input(field('part2-name'), { target: { value: '' } });
      await vi.advanceTimersByTimeAsync(600);
      setMtdDeviceName('hi_sfc');
      click('Recalculate');

      expect(mtdparts()).not.toContain('kernel');
      expect(mtdparts()).toContain('64k@0x40000(env)');
      // rootfs stays where the address columns put it, gap and all.
      expect(mtdparts()).toContain('5120k@0x250000(rootfs)');
      expect(addresses()).toContain('0x250000');
    } finally {
      vi.useRealTimers();
    }
  });

  test('a partition with a size but no name is left out of the line', async () => {
    vi.useFakeTimers();
    try {
      render(<FirmwarePartitionCalculator />);
      click('Lite');
      fireEvent.input(field('part4-name'), { target: { value: '' } });
      await vi.advanceTimersByTimeAsync(600);
      setMtdDeviceName('hi_sfc');
      click('Recalculate');

      // `704k()` is not a partition definition, so it is left out.
      expect(mtdparts()).not.toContain('()');
      expect(mtdparts()).toContain('5120k@0x250000(rootfs)');
    } finally {
      vi.useRealTimers();
    }
  });
});
