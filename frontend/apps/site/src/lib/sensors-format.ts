/**
 * How a sensor's numbers read on the page, and the colour each maker is drawn
 * in. Shared by the catalogue island and each sensor's static page, so a value
 * reads the same in the chart, the table and the spec sheet.
 */
import { megapixels, sensitivityPerArea, type Hdr, type Interface, type Measure, type Sensor } from './sensors';

/**
 * Categorical slots in a fixed order (the dataviz reference palette, validated
 * for colour-blind separation on adjacent pairs). The makers with the most
 * parts take the first slots; the rest share the neutral "other". The maker
 * is also written in every tile's hover card and the legend, so colour never
 * carries it alone.
 */
const SLOTS: Record<string, string> = {
  sony: '#2a78d6',
  smartsens: '#eb6834',
  omnivision: '#1baf7a',
  galaxycore: '#eda100',
  soi: '#e87ba4',
  onsemi: '#008300',
  imagedesign: '#4a3aa7',
};
export const OTHER_COLOUR = '#8a8f9c';

export const makerColour = (urlname: string): string => SLOTS[urlname] ?? OTHER_COLOUR;
export const hasOwnColour = (urlname: string): boolean => urlname in SLOTS;

const trim = (n: number, digits: number) => Number(n.toFixed(digits)).toString();

export const fmtMp = (s: Sensor): string | null => {
  const mp = megapixels(s);
  return mp === null ? null : `${trim(mp, mp < 10 ? 1 : 0)} MP`;
};
export const fmtResolution = (s: Sensor): string | null => (s.width && s.height ? `${s.width} × ${s.height}` : null);
export const fmtPixel = (s: Sensor): string | null => (s.pixel_um ? `${s.pixel_um} µm` : null);
export const fmtFormat = (s: Sensor): string | null => {
  if (!s.optical_format) return null;
  return /^[\d/.]+$/.test(s.optical_format) ? `${s.optical_format}″` : s.optical_format;
};
export const fmtDb = (v: number | undefined): string | null => (v ? `${trim(v, 1)} dB` : null);
export const fmtMeasure = (m: Measure | undefined): string | null => (m ? `${m.value.toLocaleString('en-US')} ${m.unit}` : null);
export const fmtPerArea = (s: Sensor): string | null => {
  const v = sensitivityPerArea(s);
  return v === null ? null : `${Math.round(v).toLocaleString('en-US')} mV/lux·s/µm²`;
};
export const fmtFps = (s: Sensor): string | null => (s.max_fps ? `${s.max_fps} fps` : null);

const HDR_NAMES: Record<Hdr, string> = {
  dol: 'DOL', stagger: 'Staggered', dcg: 'DCG', 'clear-hdr': 'Clear HDR', lofic: 'LOFIC', 'row-interleave': 'Row-interleaved', knee: 'Knee',
  'multi-exposure': 'Multi-exposure', 'single-frame': 'Single-frame', unspecified: 'Mode not stated',
};
const INTERFACE_NAMES: Record<Interface, string> = {
  mipi: 'MIPI CSI-2', lvds: 'LVDS', sublvds: 'SubLVDS', 'slvs-ec': 'SLVS-EC', hispi: 'HiSPi', dvp: 'DVP',
};
export const fmtHdr = (s: Sensor): string | null => (s.hdr?.length ? s.hdr.map((h) => HDR_NAMES[h]).join(', ') : null);
export const fmtInterfaces = (s: Sensor): string | null =>
  (s.interfaces?.length ? s.interfaces.map((i) => INTERFACE_NAMES[i]).join(', ') : null);
export const interfaceName = (i: Interface): string => INTERFACE_NAMES[i];
