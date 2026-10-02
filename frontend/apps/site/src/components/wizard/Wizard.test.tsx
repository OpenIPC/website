// @vitest-environment jsdom
/**
 * Where the form opens, rendered (#370).
 *
 * Two effects run when the export arrives: one sets the settings a shared
 * link asks for, the other keeps the settings on what the menus settled. The
 * second was computed from the defaults and landed after the first, so every
 * permanent link reopened on NOR 8M and Lite, keeping only its addresses --
 * on production, for every SoC. The pure tests cannot see an ordering of
 * effects, so this renders the island with a recorded export behind fetch.
 */
import { afterEach, describe, expect, test, vi } from 'vitest';
import { cleanup, render, waitFor } from '@testing-library/preact';
import fixture from '../../lib/wizard-open.fixture.json';
import Wizard from './Wizard.tsx';

const facts = {
  fullName: 'SoC', model: 'SoC', status: 'done', statusTitle: '', stageSrc: '', vendorName: 'Vendor',
  vendorHref: '/v', socHref: '/s', vendorsHref: '/vs', homeHref: '/', stagesHref: '/st', segment: 'cctv',
};
const links = { business: '/business', donate: '/donate', wall: '/open-wall', community: '/community', lowLatency: '/low-latency' };
const labels = { count: '', countMet: '', meter: '', monthly: '', split: '' };

function open(soc: keyof typeof fixture.socs, search: string) {
  const doc = { bl_url: '', published: [], ...fixture.socs[soc] };
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(doc))));
  window.history.replaceState({}, '', `/cameras/x/${soc}${search}`);
  return render(
    <Wizard locale="en" source={`/api/v1/wizard/${soc}.json`} facts={facts} links={links} here={`/cameras/x/${soc}`}
      supportGoal={0} supportLabels={labels} warningSrc="" />,
  );
}

const value = (container: Element, field: string) =>
  (container.querySelector(`select[name="camera[${field}]"]`) as HTMLSelectElement | null)?.value;

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('the form opens where it was asked to', () => {
  test('a permanent link keeps its chip, layout and edition', async () => {
    const { container } = open('hi3516ev300', '?mac=&cip=10.0.0.5&sip=10.0.0.1&net=&rom=nor16m&part=nor16m&ver=ultimate&sd=');
    await waitFor(() => expect(value(container, 'firmware_version')).toBe('ultimate'));
    expect(value(container, 'flash_type')).toBe('nor16m');
    expect(value(container, 'partition_layout')).toBe('nor16m');
    expect((container.querySelector('input[name="camera[camera_ip_address]"]') as HTMLInputElement).value).toBe('10.0.0.5');
  });

  test('a link to NAND opens on NAND', async () => {
    const { container } = open('hi3516ev300', '?rom=nand&ver=ultimate');
    await waitFor(() => expect(value(container, 'flash_type')).toBe('nand'));
  });

  test('a bare address on a SoC whose builds need 16MB opens on 16MB', async () => {
    const { container } = open('ssc338q', '');
    await waitFor(() => expect(value(container, 'flash_type')).toBe('nor16m'));
    expect(value(container, 'partition_layout')).toBe('nor16m');
  });

  test('a link to a 32MB chip on that SoC keeps it', async () => {
    const { container } = open('ssc338q', '?rom=nor32m&ver=lite');
    await waitFor(() => expect(value(container, 'flash_type')).toBe('nor32m'));
  });

  test('a bare address on a SoC that fits 8MB still opens on 8MB', async () => {
    const { container } = open('hi3516ev300', '');
    await waitFor(() => expect(container.querySelector('select[name="camera[flash_type]"]')).not.toBeNull());
    await new Promise((r) => setTimeout(r, 50));
    expect(value(container, 'flash_type')).toBe('nor8m');
  });
});
