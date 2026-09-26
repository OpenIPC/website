/**
 * The board catalogue's pure half: filters, stats, what a card shows and asks
 * for, the search highlight, and the query string a shared link carries.
 */
import { describe, expect, test } from 'vitest';
import type { BoardFile, BoardsFile, Hit, Model } from './types';
import {
  cardFiles, cardPhotos, entries, filterBoards, filterHits, firstMissing, flashOf, formatBytes, frontPhoto,
  highlight, sensorKey, sensorOptions, socKey, socOptions, stats,
} from './model';
import { EMPTY, readQueryString, writeQueryString } from './url';

const file = (kind: BoardFile['kind'], name: string, extra: Partial<BoardFile> = {}): BoardFile => ({
  kind, name, url: `/board-files/u/${name}`, mime: 'x', bytes: 1, sha256: '0', ...extra,
});
const photo = (kind: BoardFile['kind'], name: string) => file(kind, name, { thumb_url: `/board-files/u/thumb-${name}` });

const cov = (c: Partial<Model['coverage']> = {}): Model['coverage'] => ({
  units: 1, photos: 0, pinouts: 0, flash_dumps: 0, uboot_envs: 0, boot_logs: 0, documents: 0, ...c,
});

const model = (id: string, over: Partial<Model> = {}, sensor: string | null = null, files: BoardFile[] = []): Model => ({
  id, model: id.toUpperCase(), soc: null, soc_label: null, family: null, notes: null, coverage: cov(),
  units: [{ id: `${id}-u1`, sensor, flash_chip: null, flash_size_mb: null, source: 's', source_ref: 'r', contributed_by: 'c', notes: null, files }],
  ...over,
});

const FILE: BoardsFile = {
  schema: 1,
  files_prefix: '/board-files/',
  sources: [],
  manufacturers: [
    { id: 'xiongmai', name: 'Xiongmai', aliases: ['XM'], website: null, models: [
      model('a', { soc: 'hi3516cv300', soc_label: 'hi3516cv300', coverage: cov({ photos: 2, pinouts: 1, flash_dumps: 1, uboot_envs: 1 }) }, 'SONY IMX323'),
      model('b', { soc: 'hi3516ev100', soc_label: 'Hi3516ev100', coverage: cov({ photos: 2, flash_dumps: 2 }) }, 'Sony imx323'),
    ] },
    { id: 'unknown', name: 'Unidentified maker', aliases: [], website: null, models: [
      model('c', { model: null, soc_label: 'hi3559av100', family: 'hi3559av100' }, 'OnmiVision OV4689'),
      model('d', { model: null, family: 'hi3516cv200' }),
    ] },
  ],
};
const ALL = entries(FILE);

describe('filters', () => {
  test('entries keep their maker', () => {
    expect(ALL.map((m) => `${m.maker.id}/${m.id}`)).toEqual(['xiongmai/a', 'xiongmai/b', 'unknown/c', 'unknown/d']);
  });

  test('a sensor is one sensor however its maker is spelled', () => {
    expect(sensorKey('SONY IMX323')).toBe('IMX323');
    expect(sensorKey('Sony imx323')).toBe('IMX323');
    expect(sensorKey('OnmiVision OV4689')).toBe('OV4689');
    expect(sensorKey('Silicon Optronics H22')).toBe('H22');
    expect(sensorKey(null)).toBeNull();
    expect(sensorOptions(ALL)).toEqual([['IMX323', 'IMX323'], ['OV4689', 'OV4689']]);
  });

  test('a SoC is the catalogue urlname, else the source label, else nothing', () => {
    expect(ALL.map(socKey)).toEqual(['hi3516cv300', 'hi3516ev100', 'hi3559av100', null]);
    expect(socOptions(ALL, { hi3516cv300: 'HI3516CV300', hi3516ev100: 'HI3516EV100' })).toEqual([
      ['hi3516cv300', 'HI3516CV300'], ['hi3516ev100', 'HI3516EV100'], ['hi3559av100', 'HI3559AV100'],
    ]);
  });

  test('each filter narrows, and they combine', () => {
    const ids = (s: Partial<typeof EMPTY>) => filterBoards(ALL, { ...EMPTY, ...s }).map((m) => m.id);
    expect(ids({})).toEqual(['a', 'b', 'c', 'd']);
    expect(ids({ maker: 'unknown' })).toEqual(['c', 'd']);
    expect(ids({ soc: 'hi3516cv300' })).toEqual(['a']);
    expect(ids({ soc: 'hi3559av100' })).toEqual(['c']);
    expect(ids({ sensor: 'IMX323' })).toEqual(['a', 'b']);
    expect(ids({ missing: 'pinout' })).toEqual(['b', 'c', 'd']);
    expect(ids({ missing: 'flash_dump', maker: 'xiongmai' })).toEqual([]);
    expect(ids({ sensor: 'IMX323', missing: 'uboot_env' })).toEqual(['b']);
  });

  test('search hits are kept only for boards the filters leave', () => {
    const hit = (model_id: string) => ({ model_id } as Hit);
    const kept = filterBoards(ALL, { ...EMPTY, maker: 'xiongmai' });
    expect(filterHits([hit('a'), hit('c'), hit('b')], kept).map((h) => h.model_id)).toEqual(['a', 'b']);
  });
});

describe('stats', () => {
  test('counts boards, named makers, pinouts and dumps', () => {
    expect(stats(ALL)).toEqual({ boards: 4, makers: 1, pinouts: 1, dumps: 3, needPinout: 3 });
  });
});

describe('a card', () => {
  const m = model('e', {}, null, [
    photo('photo_other', 'o.jpg'), photo('photo_front', 'f1.jpg'), photo('photo_front', 'f2.jpg'),
    photo('pinout', 'p.jpg'), photo('photo_back', 'b.jpg'), photo('photo_other', 'o2.jpg'),
    file('flash_dump', 'd.bin', { bytes: 8388608 }), file('uboot_env', 'x.uboot', { lines: 87 }), file('document', 'm.pdf'),
  ]);

  test('shows one of each picture first, then fills to four', () => {
    expect(cardPhotos(m).map((f) => f.name)).toEqual(['f1.jpg', 'b.jpg', 'p.jpg', 'o.jpg']);
    expect(cardPhotos(m, 6).map((f) => f.name)).toEqual(['f1.jpg', 'b.jpg', 'p.jpg', 'o.jpg', 'f2.jpg', 'o2.jpg']);
    expect(frontPhoto(m)?.name).toBe('f1.jpg');
    expect(frontPhoto(model('none'))).toBeNull();
  });

  test('lists every file that is not a picture', () => {
    expect(cardFiles(m).map((f) => f.kind)).toEqual(['flash_dump', 'uboot_env', 'document']);
  });

  test('asks for a pinout first and a boot log only once nothing else is missing', () => {
    expect(firstMissing(ALL[0])).toBe('boot_log');
    expect(firstMissing(ALL[1])).toBe('pinout');
    expect(firstMissing(ALL[2])).toBe('pinout');
    expect(firstMissing(model('p', { coverage: cov({ pinouts: 1 }) }))).toBe('photos');
    expect(firstMissing(model('full', { coverage: cov({ photos: 1, pinouts: 1, flash_dumps: 1, uboot_envs: 1, boot_logs: 1 }) }))).toBeNull();
  });

  test('flash is the chip and its size, whichever it knows', () => {
    const f = (flash_chip: string | null, flash_size_mb: number | null) =>
      flashOf({ ...m, units: [{ ...m.units[0], flash_chip, flash_size_mb }] });
    expect(f('MX25L6406E', 8)).toBe('MX25L6406E, 8 MB');
    expect(f(null, 16)).toBe('16 MB');
    expect(f(null, null)).toBeNull();
  });

  test('sizes read in the page\'s language', () => {
    expect(formatBytes(8388608, 'en')).toBe('8 MB');
    expect(formatBytes(129504, 'en')).toBe('126 KB');
    expect(formatBytes(1572864, 'ru')).toBe('1,5 MB');
  });
});

describe('highlight', () => {
  test('marks every match, case-insensitively, keeping the original case', () => {
    expect(highlight('Block:64KB Chip:8MB Name:"XM25QH64A" xm25qh64a', 'xm25qh64a')).toEqual([
      { text: 'Block:64KB Chip:8MB Name:"', mark: false },
      { text: 'XM25QH64A', mark: true },
      { text: '" ', mark: false },
      { text: 'xm25qh64a', mark: true },
    ]);
  });

  test('regex characters are literal', () => {
    expect(highlight('a.b axb', 'a.b')).toEqual([{ text: 'a.b', mark: true }, { text: ' axb', mark: false }]);
  });
});

describe('the query string', () => {
  test('a bare address is the default view, and writes back as nothing', () => {
    expect(readQueryString('')).toEqual(EMPTY);
    expect(writeQueryString(EMPTY)).toBe('');
  });

  test('round-trips every field', () => {
    const s = { q: 'xm25qh64a', scope: 'all' as const, maker: 'hsell', soc: 'hi3516cv300', sensor: 'IMX323', missing: 'pinout' as const };
    expect(writeQueryString(s)).toBe('?q=xm25qh64a&scope=all&maker=hsell&soc=hi3516cv300&sensor=IMX323&missing=pinout');
    expect(readQueryString(writeQueryString(s))).toEqual(s);
  });

  test('drops what it does not recognise', () => {
    expect(readQueryString('?scope=everything&missing=soul&soc=HI3516CV300')).toEqual({ ...EMPTY, soc: 'hi3516cv300' });
  });
});
