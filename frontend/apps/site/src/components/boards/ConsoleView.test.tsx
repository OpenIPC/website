// @vitest-environment jsdom
/**
 * A console opened in place: the camera's own colours, the facts and the
 * flash map over it, line numbers that link to a line, and a link that opens
 * the file at its line.
 */
import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/preact';
import { useBoardsTranslations } from '../../lib/boards-i18n';

const texts: Record<string, string> = {
  '/board-files/u1/a.uboot': [
    'U-Boot 2010.06-svn (Oct 14 2015 - 15:05:28)',
    'DRAM:  256 MiB',
    'Spi(cs1): Block:64KB Chip:8MB Name:"MX25L6406E"',
    'hisilicon # printenv',
    'bootargs=mem=64M console=ttyAMA0,115200 mtdparts=hi_sfc:256K(boot),3520K(romfs),-(rest)',
    '\x1b[1;31mERROR\x1b[0m: something',
  ].join('\n'),
};
vi.mock('../../lib/boards/api', () => ({ fetchText: (url: string) => Promise.resolve(texts[url] ?? '') }));
const { default: ConsoleView } = await import('./ConsoleView');

const t = useBoardsTranslations('en');
afterEach(cleanup);

test('closed until asked; opened, it shows the facts, the flash map and every line numbered', async () => {
  const view = render(<ConsoleView url="/board-files/u1/a.uboot" name="a.uboot" label="U-Boot console" lines={6} t={t} />);
  expect(view.queryByRole('region')).toBeNull();
  fireEvent.click(view.getByRole('button', { name: 'U-Boot console · a.uboot' }));
  const log = await view.findByRole('region', { name: 'U-Boot console: a.uboot' });
  expect(log.querySelectorAll('[data-ln]')).toHaveLength(6);
  expect(view.getByText('MX25L6406E · 8 MB')).toBeTruthy();
  expect(view.getByText('2010.06-svn (Oct 14 2015 - 15:05:28)')).toBeTruthy();
  // The partitions, with the last one sized to the rest of the 8 MB chip.
  expect(view.getAllByText('romfs')).toHaveLength(2); // in the bar and in the list
  expect(view.getByText('4416 KB (the rest of the chip)')).toBeTruthy();
});

test('the camera\'s own colour is kept; a line without colour is highlighted', async () => {
  const view = render(<ConsoleView url="/board-files/u1/a.uboot" name="a.uboot" label="U-Boot console" t={t} open />);
  const log = await view.findByRole('region');
  const error = [...log.querySelectorAll('span')].find((s) => s.textContent === 'ERROR');
  expect(error?.getAttribute('style')).toMatch(/color: (#e06c75|rgb\(224, 108, 117\))/);
  expect(error?.getAttribute('style')).toMatch(/font-weight: 600/);
  const prompt = [...log.querySelectorAll('span')].find((s) => s.textContent === 'hisilicon # ');
  expect(prompt?.className).toMatch(/text-\[#82c4ff\]/);
});

test('a link to a line opens the file there, marked; a line number clicked is reported', async () => {
  const onLine = vi.fn();
  const scrolled = vi.fn();
  HTMLElement.prototype.scrollIntoView = scrolled;
  const view = render(<ConsoleView url="/board-files/u1/a.uboot" name="a.uboot" label="U-Boot console" t={t} focusLine={4} onLine={onLine} query="printenv" />);
  const log = await view.findByRole('region');
  await waitFor(() => expect(log.querySelector('[data-ln="4"]')?.className).toMatch(/bg-\[#2a3456\]/));
  await waitFor(() => expect(scrolled).toHaveBeenCalled()); // effects run after paint
  expect(log.querySelector('mark')?.textContent).toBe('printenv');
  fireEvent.click(view.getByRole('link', { name: 'Line 2' }));
  expect(onLine).toHaveBeenCalledWith(2);
  expect(log.querySelector('[data-ln="2"]')?.className).toMatch(/bg-\[#2a3456\]/);
});
