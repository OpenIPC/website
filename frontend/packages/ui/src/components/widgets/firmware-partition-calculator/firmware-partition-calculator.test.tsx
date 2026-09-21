/**
 * The calculator's arithmetic, locked.
 *
 * Its output is an `mtdparts` line a reader pastes into a bootloader, so a
 * wrong offset here is a bricked camera rather than a layout bug. The
 * expected addresses below are computed by hand from the presets and checked
 * against the component -- not snapshotted out of it.
 */
import { expect, test, describe } from 'vitest';
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
    .find(t => t.includes('k(')) ?? '';
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

    expect(mtdparts()).toBe(
      'hi_sfc:256k(boot),64k(env),2048k(kernel),5120k(rootfs),704k(rootfs_data)',
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
