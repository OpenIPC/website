// @vitest-environment jsdom
/**
 * The SoC box, rendered: typing narrows the list to what contains the text,
 * however slowly it is typed, and the keyboard alone can pick a chip.
 */
import { afterEach, beforeAll, describe, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render } from '@testing-library/preact';
import { useExplorerTranslations } from '../../lib/explorer-i18n';
import SocPicker from './SocPicker';

const t = useExplorerTranslations('en');
const groups = [
  { vendor: 'SigmaStar', socs: ['ssc333', 'ssc335', 'ssc335de', 'ssc338q'] },
  { vendor: 'Fullhan', socs: ['fh8833v100', 'fh8852v100'] },
];

beforeAll(() => {
  // jsdom lays nothing out.
  Element.prototype.scrollIntoView = () => {};
});
afterEach(cleanup);

function mount(value: string | null = 'ssc333') {
  const onChange = vi.fn();
  const r = render(<SocPicker id="soc" labelId="soc-label" groups={groups} value={value} onChange={onChange} t={t} />);
  const box = r.getByRole('combobox') as HTMLInputElement;
  const options = () => r.queryAllByRole('option').map((o) => o.textContent);
  const active = () => r.queryAllByRole('option').find((o) => o.getAttribute('aria-selected') === 'true')?.textContent;
  return { ...r, box, options, active, onChange };
}

describe('the SoC box', () => {
  test('opens on focus with every chip, the current one active', () => {
    const { box, options, active } = mount();
    fireEvent.focus(box);
    expect(options()).toEqual(['ssc333', 'ssc335', 'ssc335de', 'ssc338q', 'fh8833v100', 'fh8852v100']);
    expect(active()).toBe('ssc333');
  });

  test('typing s-s-c-3-3-5 one key at a time never offers a Fullhan, and Enter picks ssc335', () => {
    const { box, options, active, onChange } = mount();
    fireEvent.focus(box);
    let typed = '';
    for (const ch of 'ssc335') {
      typed += ch;
      fireEvent.input(box, { target: { value: typed } });
      expect(options().some((o) => o?.startsWith('fh'))).toBe(false);
    }
    expect(options()).toEqual(['ssc335', 'ssc335de']);
    expect(active()).toBe('ssc335');
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onChange).toHaveBeenCalledWith('ssc335');
    expect(options()).toEqual([]);
  });

  test('the middle of a name matches, and the arrows move through what is shown', () => {
    const { box, options, active, onChange } = mount();
    fireEvent.focus(box);
    fireEvent.input(box, { target: { value: '335' } });
    expect(options()).toEqual(['ssc335', 'ssc335de']);
    fireEvent.keyDown(box, { key: 'ArrowDown' });
    expect(active()).toBe('ssc335de');
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onChange).toHaveBeenCalledWith('ssc335de');
  });

  test('Escape closes without picking and shows the current chip again', () => {
    const { box, options, onChange } = mount();
    fireEvent.focus(box);
    fireEvent.input(box, { target: { value: 'fh' } });
    fireEvent.keyDown(box, { key: 'Escape' });
    expect(options()).toEqual([]);
    expect(box.value).toBe('ssc333');
    expect(onChange).not.toHaveBeenCalled();
  });

  test('nothing matching says so; a click picks', () => {
    const { box, getByText, getByRole, onChange } = mount();
    fireEvent.focus(box);
    fireEvent.input(box, { target: { value: 'zzz' } });
    getByText('No SoC matches');
    fireEvent.input(box, { target: { value: 'fh88' } });
    fireEvent.mouseDown(getByRole('option', { name: 'fh8852v100' }));
    expect(onChange).toHaveBeenCalledWith('fh8852v100');
  });
});
