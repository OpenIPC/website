/**
 * The board catalogue's pure half: filters, stats, the gallery's layout, what
 * a card shows and asks for, model codes linked in prose, the search
 * highlight, and the query string a shared link carries.
 */
import { describe, expect, test } from 'vitest';
import type { BoardFile, BoardsFile, Hit, Model } from './types';
import {
  lineLabel,
  cardFiles, cardPhotos, codeIndex, entries, filterBoards, filterHits, firstMissing, flashOf, formatBytes, frontPhoto,
  highlight, layout, lead, lineOptions, linkCodes, lines, normaliseCode, paragraphs, sensorKey, sensorOptions, slug,
  socKey, socOptions, stats, subtitle, unitFiles, unitPhotos,
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
  id, model: id.toUpperCase(), soc: null, soc_label: null, family: null, notes: null,
  category: null, tags: [], summary: null, sources: ['s'], coverage: cov(),
  units: [{ id: `${id}-u1`, sensor, flash_chip: null, flash_size_mb: null, source: 's', source_ref: 'r', contributed_by: 'c', notes: null, files }],
  ...over,
});

const FILE: BoardsFile = {
  schema: 1,
  locale: 'en',
  files_prefix: '/board-files/',
  sources: [],
  manufacturers: [
    { id: 'xiongmai', name: 'Xiongmai', aliases: ['XM'], website: null, models: [
      model('a', { soc: 'hi3516cv300', soc_label: 'hi3516cv300', category: 'IP Camera Module', tags: ['openipc-ready'],
        sources: ['openhisiipcam', 'xiongmai'], coverage: cov({ photos: 2, pinouts: 1, flash_dumps: 1, uboot_envs: 1 }) }, 'SONY IMX323'),
      model('b', { soc: 'hi3516ev100', soc_label: 'Hi3516ev100', category: 'NVR Board', tags: ['discontinued'],
        sources: ['cctvsp'], coverage: cov({ photos: 2, flash_dumps: 2 }) }, 'Sony imx323'),
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

  test('product line, source and OpenIPC-ready narrow too', () => {
    const ids = (s: Partial<typeof EMPTY>) => filterBoards(ALL, { ...EMPTY, ...s }).map((m) => m.id);
    expect(ids({ line: 'NVR Board' })).toEqual(['b']);
    expect(ids({ line: 'Nothing' })).toEqual([]);
    expect(ids({ source: 'xiongmai' })).toEqual(['a']);
    expect(ids({ source: 'cctvsp' })).toEqual(['b']);
    expect(ids({ source: 's' })).toEqual(['c', 'd']);
    expect(ids({ ready: true })).toEqual(['a']);
    expect(ids({ ready: true, line: 'NVR Board' })).toEqual([]);
    expect(lineOptions(ALL)).toEqual([['IP Camera Module', 'IP Camera Module'], ['NVR Board', 'NVR Board']]);
  });

  test('search hits are kept only for boards the filters leave', () => {
    const hit = (model_id: string) => ({ model_id } as Hit);
    const kept = filterBoards(ALL, { ...EMPTY, maker: 'xiongmai' });
    expect(filterHits([hit('a'), hit('c'), hit('b')], kept).map((h) => h.model_id)).toEqual(['a', 'b']);
  });
});

describe('the layout', () => {
  const xm = (n: number, line: string | null) => Array.from({ length: n }, (_, i) =>
    model(`${line ?? 'none'}-${i}`, { category: line }));
  const big: BoardsFile = {
    ...FILE,
    manufacturers: [
      { id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null,
        models: [...xm(3, 'DVR Board'), ...xm(2, null), ...xm(5, 'NVR Board'), ...xm(1, 'AF module')] },
      { id: 'hsell', name: 'HSELL', aliases: [], website: null, models: xm(4, 'IP Camera Module').map((m) => ({ ...m, id: `hsell-${m.id}` })) },
      { id: 'empty', name: 'Nobody', aliases: [], website: null, models: [] },
    ],
  };
  const all = entries(big);

  test('a big maker is split by product line, biggest first and the unfiled last', () => {
    const [xiongmai, hsell] = layout(big.manufacturers, all, 5);
    expect(xiongmai.maker.id).toBe('xiongmai');
    expect(xiongmai.count).toBe(11);
    expect(xiongmai.groups.map((g) => [g.label, g.entries.length])).toEqual([
      ['NVR Board', 5], ['DVR Board', 3], ['AF module', 1], [null, 2],
    ]);
    expect(xiongmai.groups.map((g) => g.key)).toEqual(['xiongmai-nvr-board', 'xiongmai-dvr-board', 'xiongmai-af-module', 'xiongmai-other']);
    expect(hsell.groups).toHaveLength(1);
    expect(hsell.groups[0].label).toBeNull();
  });

  test('a maker the filters empty is left out, and a small one is one group', () => {
    const sections = layout(big.manufacturers, filterBoards(all, { ...EMPTY, line: 'NVR Board' }), 5);
    expect(sections.map((s) => s.maker.id)).toEqual(['xiongmai']);
    expect(sections[0].groups).toHaveLength(1);
  });

  test('renders every kept board exactly once, under a unique key', () => {
    for (const split of [1, 5, 40]) {
      const sections = layout(big.manufacturers, all, split);
      const ids = sections.flatMap((s) => s.groups.flatMap((g) => g.entries.map((m) => m.id)));
      expect(new Set(ids).size, `split over ${split}`).toBe(ids.length);
      expect(ids.sort()).toEqual(all.map((m) => m.id).sort());
      const keys = sections.flatMap((s) => s.groups.map((g) => g.key));
      expect(new Set(keys).size).toBe(keys.length);
    }
  });

  test('headings get anchors, whatever the script', () => {
    expect(slug('XVI&AHD Hybrid Camera Module')).toBe('xvi-ahd-hybrid-camera-module');
    expect(slug('智能分析模组')).toBe('智能分析模组');
    expect(slug('--')).toBe('line');
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
    expect(unitFiles([...m.units[0].files, file('firmware', 'fw.bin')]).map((f) => f.kind)).toEqual(['flash_dump', 'uboot_env', 'document', 'firmware']);
    expect(unitPhotos(m.units[0].files)).toHaveLength(6);
  });

  test('its subtitle is the summary name, unless that only repeats the code', () => {
    const s = (summary: Model['summary'], code: string | null = 'IPG-50H20PLS-S') => subtitle(model('x', { model: code, summary }));
    const sum = (name: string | null, lead: string | null = null) => ({ name, lead, locale: 'en', translated_from: null });
    expect(s(null)).toBeNull();
    expect(s(sum(null))).toBeNull();
    expect(s(sum('  '))).toBeNull();
    expect(s(sum('2.0M CMOS IP Camera Module'))).toBe('2.0M CMOS IP Camera Module');
    expect(s(sum('ipg 50h20pls s'))).toBeNull();
    expect(s(sum('Anything'), null)).toBe('Anything');
    expect(lead(model('x', { summary: sum('n', ' Module for a camera. ') }))).toBe('Module for a camera.');
    expect(lead(model('x', { summary: sum('n', '') }))).toBeNull();
    expect(lead(model('x'))).toBeNull();
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

describe('model codes in prose', () => {
  const boards = [
    { id: 'xiongmai-ipg-50h20pls-s', model: 'IPG-50H20PLS-S' },
    { id: 'xiongmai-ipg-80h20pt-s', model: 'IPG-80H20PT-S' },
    { id: 'xiongmai-ipg-50h20pl-a', model: 'IPG-50H20PL-A' },
    { id: 'xiongmai-ipg-50h20pl-as', model: 'IPG-50H20PL-AS' },
    { id: 'jvt-s290', model: 'S290H16XF/S291H16XF' },
    { id: 'xiongmai-53h20-s', model: '53h20-s' },
    { id: 'short', model: 'RB4' },
    { id: 'twin-1', model: 'TWIN-01' },
    { id: 'twin-2', model: 'twin 01' },
    { id: 'nameless', model: null },
  ];
  const index = codeIndex(boards);
  const self = boards[0];
  const linked = (text: string, me = self) => linkCodes(text, index, me).filter((p) => p.id).map((p) => [p.text, p.id]);

  test('codes are compared upper-cased, with spaces, underscores, slashes and dots as hyphens', () => {
    expect(normaliseCode(' ipg_50h20pls.s ')).toBe('IPG-50H20PLS-S');
    expect(normaliseCode('S290H16XF/S291H16XF')).toBe('S290H16XF-S291H16XF');
  });

  test('links another board named in a description, as written', () => {
    const text = 'Рекомендуемая производителем замена - IPG-80H20PT-S, на более новом сенсоре F22.';
    expect(linked(text)).toEqual([['IPG-80H20PT-S', 'xiongmai-ipg-80h20pt-s']]);
    expect(linkCodes(text, index, self).map((p) => p.text).join('')).toBe(text);
    expect(linked('see ipg 80h20pt s or IPG_80H20PT_S')).toEqual([
      ['ipg 80h20pt s', 'xiongmai-ipg-80h20pt-s'], ['IPG_80H20PT_S', 'xiongmai-ipg-80h20pt-s'],
    ]);
    expect(linked('S290H16XF/S291H16XF and 53H20-S')).toEqual([['S290H16XF/S291H16XF', 'jvt-s290'], ['53H20-S', 'xiongmai-53h20-s']]);
  });

  test('never links the board\'s own code', () => {
    expect(linked('IPG-50H20PLS-S: 1/2.7" CMOS sensor')).toEqual([]);
    expect(linked('ipg-50h20pls-s', { id: 'another-id', model: 'IPG 50H20PLS S' })).toEqual([]);
    expect(linkCodes('IPG-50H20PLS-S and IPG-80H20PT-S', index, self).map((p) => p.text)).toEqual(['IPG-50H20PLS-S and ', 'IPG-80H20PT-S']);
  });

  test('the longest code wins, and a code inside a longer word is not one', () => {
    expect(linked('IPG-50H20PL-AS / IPG-50H20PL-A / IPG-50H20PL-B')).toEqual([
      ['IPG-50H20PL-AS', 'xiongmai-ipg-50h20pl-as'], ['IPG-50H20PL-A', 'xiongmai-ipg-50h20pl-a'],
    ]);
    expect(linked('XIPG-80H20PT-S IPG-80H20PT-S2 IPG-80H20PT-SЕ')).toEqual([]);
  });

  test('short codes and codes two boards share are left alone', () => {
    expect(linked('RB4 TWIN-01')).toEqual([]);
    expect(codeIndex([]).re).toBeNull();
    expect(linkCodes('text', codeIndex([]), self)).toEqual([{ text: 'text' }]);
    expect(linkCodes('', index, self)).toEqual([]);
  });

  test('descriptions split into paragraphs at blank lines, features into lines', () => {
    expect(paragraphs('One.\n\nTwo\nstill two.\n \nThree.')).toEqual(['One.', 'Two\nstill two.', 'Three.']);
    expect(paragraphs(null)).toEqual([]);
    expect(lines('a\n\n b \n')).toEqual(['a', 'b']);
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
    const s = {
      q: 'xm25qh64a', scope: 'all' as const, maker: 'hsell', soc: 'hi3516cv300', sensor: 'IMX323', missing: 'pinout' as const,
      line: 'XVI&AHD Hybrid Camera Module', source: 'cctvsp', ready: true, model: 'xiongmai-ipg-50h20pls-s',
    };
    expect(writeQueryString(s)).toBe('?q=xm25qh64a&scope=all&maker=hsell&soc=hi3516cv300&sensor=IMX323&missing=pinout'
      + '&line=XVI%26AHD+Hybrid+Camera+Module&source=cctvsp&ready=1&model=xiongmai-ipg-50h20pls-s');
    expect(readQueryString(writeQueryString(s))).toEqual(s);
  });

  test('a board alone is a deep link to its details', () => {
    expect(writeQueryString({ ...EMPTY, model: 'xiongmai-ipg-50h20pls-s' })).toBe('?model=xiongmai-ipg-50h20pls-s');
    expect(readQueryString('?model=xiongmai-ipg-50h20pls-s')).toEqual({ ...EMPTY, model: 'xiongmai-ipg-50h20pls-s' });
  });

  test('drops what it does not recognise', () => {
    expect(readQueryString('?scope=everything&missing=soul&soc=HI3516CV300&ready=yes&model=')).toEqual({ ...EMPTY, soc: 'hi3516cv300' });
  });
});

describe('product lines', () => {
  test('a line the locale files know reads in the reader\'s language; others as the source names them', () => {
    const t = (key: string) => ({ 'product_line.nvr-board': 'Платы NVR' } as Record<string, string>)[key] ?? key;
    expect(lineLabel('NVR Board', t)).toBe('Платы NVR');
    expect(lineLabel('Something New', t)).toBe('Something New');
    expect(slug('XVI&AHD Hybrid Camera Module')).toBe('xvi-ahd-hybrid-camera-module');
    expect(slug('H.265 XVI DVR Board')).toBe('h-265-xvi-dvr-board');
  });
});
