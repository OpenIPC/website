/**
 * The board catalogue's pure half: filters, stats, the gallery's layout, what
 * a card shows and asks for, model codes linked in prose, the search
 * highlight, and the query string a shared link carries.
 */
import { describe, expect, test } from 'vitest';
import type { BoardFile, BoardsFile, Hit, Model } from './types';
import {
  lineLabel,
  addsIPeye, bySeller, cardFiles, formatDay, foundIn, insideOf, cardPhotos, codeIndex, couplerDevices, deviceIdOf, entries, kindOf, tally, filterBoards, filterHits, heading, matchBoards, newestFirst, printedCode, firstMissing, flashOf, formatBytes, frontPhoto,
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

describe('finding a board by what it is called', () => {
  const summary = (name: string) => ({ name, lead: null, locale: 'en', translated_from: null });
  const list = entries({
    ...FILE,
    manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null, models: [
      model('zoom', { model: 'JZC-N81820S', aliases: ['JZC-N81820'], summary: summary('Starlight 2.0M 18X AF module'), category: 'AF Module' }),
      model('ipg', { model: 'IPG-50H20PLS-S', summary: summary('2MP IP camera module'), soc_label: 'Hi3516CV300' }),
      model('near', { model: 'IPG-50H20PL-S', summary: summary('2MP IP camera module') }),
      model('old', { model: '53H20-S', soc: 'hi3516cv100', soc_label: 'hi3516c' }, 'SONY IMX222'),
    ] }],
  });
  const ids = (q: string) => matchBoards(list, q).map((m) => m.id);

  test('by code, with or without separators, in any case', () => {
    expect(ids('JZC-N81820S')).toEqual(['zoom']);
    expect(ids('jzc n81820s')).toEqual(['zoom']);
    expect(ids('N81820')).toEqual(['zoom']);
  });

  test('by product name, words in any order', () => {
    expect(ids('Starlight 2.0M 18X')).toEqual(['zoom']);
    expect(ids('18x starlight')).toEqual(['zoom']);
  });

  test('an exact code comes before the codes that contain it', () => {
    expect(ids('IPG-50H20PL-S')).toEqual(['near', 'ipg']);
    expect(ids('IPG-50H20PL')).toEqual(['ipg', 'near']);
    expect(ids('2MP')).toEqual(['ipg', 'near']);
  });

  test('by SoC and nothing for a word no board carries', () => {
    expect(ids('hi3516cv300')).toEqual(['ipg']);
    expect(ids('mtdparts')).toEqual([]);
  });

  test('by the catalogue SoC a source label resolved to, and by the sensor as the card shows it', () => {
    expect(ids('hi3516cv100')).toEqual(['old']);
    expect(ids('SONY IMX222')).toEqual(['old']);
    expect(ids('imx222')).toEqual(['old']);
  });
});

describe('XM device IDs', () => {
  const fw = { key: 'k', version: 'v', build: 'b', url: 'u', sha256: null, size: null, published_at: null };
  test('a query is a device ID when it reads like one off a camera', () => {
    expect(deviceIdOf(' 000559a7 ')).toBe('000559A7');
    expect(deviceIdOf('000929ZR')).toBe('000929ZR');
    expect(deviceIdOf('c2106510')).toBe('C2106510');
    expect(deviceIdOf('0003068b')).toBe('0003068B');
    expect(deviceIdOf('IPG-50H2')).toBeNull();
    expect(deviceIdOf('mtdparts')).toBeNull();
    expect(deviceIdOf('0x820000')).toBeNull();
    expect(deviceIdOf('000559A7.1')).toBeNull();
  });

  test('a board is OpenIPC-ready by the device IDs coupler has an image for', () => {
    expect(couplerDevices({ devices: [{ id: '000559A7', stock: [], coupler: fw }, { id: '00002520', stock: [fw], coupler: null }] })).toEqual(['000559A7']);
    expect(couplerDevices({})).toEqual([]);
  });

  test('searching a device ID finds the boards that run it', () => {
    const list = entries({ ...FILE, manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null, models: [
      model('ivg', { model: 'IVG-85HF20PYA-S', devices: [{ id: '000559A7', stock: [], coupler: fw }] }),
      model('other', { model: 'IPG-50H20PLS-S', devices: [{ id: '00002520', stock: [], coupler: null }] }),
    ] }] });
    expect(matchBoards(list, '000559a7').map((m) => m.id)).toEqual(['ivg']);
  });
});

describe('finished devices', () => {
  const fw = { key: 'k', version: 'v', build: 'b', url: 'u', sha256: null, size: null, published_at: null };
  const list = entries({ ...FILE, manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null, models: [
    model('nbd', { model: 'NBD80S16S-KL', devices: [{ id: 'C6380233', stock: [fw], coupler: null }] }),
    model('nvr', { model: 'NVR8016SY-SKL', kind: 'recorder', devices: [{ id: 'C6380233', stock: [fw], coupler: null }] }),
    model('cam', { model: 'JF-IPC-EO8340PG-IR4R-PA', kind: 'camera' }),
  ] }] });

  test('an entry is a board unless a source says otherwise, and the type filter narrows', () => {
    expect(list.map(kindOf)).toEqual(['board', 'recorder', 'camera']);
    expect(filterBoards(list, { ...EMPTY, kind: 'recorder' }).map((m) => m.id)).toEqual(['nvr']);
    expect(filterBoards(list, { ...EMPTY, kind: 'board' }).map((m) => m.id)).toEqual(['nbd']);
  });

  test('a total counts boards and finished devices apart', () => {
    const t = (k: string, v?: Record<string, unknown>) => `${String(v?.count)} ${k}`;
    expect(tally(list, t)).toBe('1 board_count · 2 device_count');
    expect(tally(list.slice(1), t)).toBe('2 device_count');
    expect(tally(list.slice(0, 1), t)).toBe('1 board_count');
    expect(tally([], t)).toBe('0 board_count');
  });

  test('a finished device names the board that runs its firmware; a board names none', () => {
    const [nbd, nvr, cam] = list;
    expect(insideOf(nvr, list).map((r) => [r.board?.id, r.status, r.basis, r.label])).toEqual([['nbd', 'likely', 'device_id', 'C6380233']]);
    expect(insideOf(cam, list)).toEqual([]);
    expect(insideOf(nbd, list)).toEqual([]);
    expect(foundIn(nbd, list).map((f) => [f.device.id, f.status])).toEqual([['nvr', 'likely']]);
    expect(foundIn(nvr, list)).toEqual([]);
  });

  test("the vendor's firmware makes a board most likely inside; an owner's photo settles it", () => {
    const page = 'https://download.jftech.com/d/MDAwMDE1OTM=';
    const likely = (code: string, board_id: string | null) =>
      ({ code, board_id, status: 'likely' as const, basis: 'firmware_build' as const, evidence: page, label: 'J91659N7.1IPC_GK7205V200_G4F_S38', source: 'jftech' });
    const g4f = model('g4f', { model: 'IVG-G4F', devices: [{ id: '000659N7', stock: [], coupler: null }] });
    const other = model('g4h', { model: 'IVG-G4H' });
    const cam = model('cam', { model: 'IPC-HX8340PGF-IR2R-PAT', kind: 'camera', devices: [{ id: 'J91659N7', stock: [], coupler: null }],
      contents: [likely('IVG-G4F', 'g4f'), likely('AHB80N04R-GS-V3', null)] });
    const two = entries({ ...FILE, manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null, models: [g4f, other, cam] }] });
    const [b, , c] = two;
    expect(insideOf(c, two).map((r) => [r.code, r.board?.id ?? null, r.status])).toEqual([
      ['IVG-G4F', 'g4f', 'likely'], ['AHB80N04R-GS-V3', null, 'likely']]);
    expect(foundIn(b, two).map((f) => f.device.id)).toEqual(['cam']);

    // A confirmed board, even a different one, replaces every likely one.
    const confirmed = { ...likely('IVG-G4H', 'g4h'), status: 'confirmed' as const, basis: 'owner' as const, source: 'owners' };
    const settled = entries({ ...FILE, manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null,
      models: [g4f, other, { ...cam, contents: [confirmed, likely('IVG-G4F', 'g4f')] }] }] });
    expect(insideOf(settled[2], settled).map((r) => [r.code, r.status])).toEqual([['IVG-G4H', 'confirmed']]);
    expect(foundIn(settled[0], settled)).toEqual([]);
    expect(foundIn(settled[1], settled).map((f) => [f.device.id, f.status])).toEqual([['cam', 'confirmed']]);
  });

  test('a board named by the firmware and sharing the device ID is listed once', () => {
    const nbd = model('nbd', { model: 'NBD80S16S-KL', devices: [{ id: 'C6380233', stock: [], coupler: null }] });
    const nvr = model('nvr', { model: 'NVR8016SY-SKL', kind: 'recorder', devices: [{ id: 'C6380233', stock: [], coupler: null }],
      contents: [{ code: 'NBD80S16S-KL', board_id: 'nbd', status: 'likely', basis: 'firmware_page', evidence: 'https://download.jftech.com/d/x', label: 'C6380233（NBD80S16S-KL）', source: 'jftech' }] });
    const l = entries({ ...FILE, manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null, models: [nbd, nvr] }] });
    expect(insideOf(l[1], l).map((r) => r.basis)).toEqual(['firmware_page']);
  });
});

describe('what a board is headed by', () => {
  const summary = (name: string) => ({ name, lead: null, locale: 'en', translated_from: null });

  test('its printed code; a code made up for a page without one is not shown', () => {
    expect(printedCode('IPG-50H20PLS-S')).toBe('IPG-50H20PLS-S');
    expect(printedCode('XM-EN-243')).toBeNull();
    expect(printedCode('XM-ZH-476')).toBeNull();
    expect(printedCode(null)).toBeNull();
  });

  test('a board without a printed code is headed by its name, which is then not repeated below', () => {
    const named = model('xm', { model: 'XM-EN-243', summary: summary('4ch 1080P POE Extension NVR Board') });
    expect(heading(named)).toEqual({ text: '4ch 1080P POE Extension NVR Board', kind: 'name' });
    expect(subtitle(named)).toBeNull();
    const coded = model('ipg', { model: 'IPG-50H20PLS-S', summary: summary('2MP IP camera module') });
    expect(heading(coded)).toEqual({ text: 'IPG-50H20PLS-S', kind: 'code' });
    expect(subtitle(coded)).toBe('2MP IP camera module');
    expect(heading(model('none', { model: null }))).toEqual({ text: null, kind: 'none' });
  });
});

describe('newest boards first', () => {
  const ids = (list: Model[]) => newestFirst(list).map((m) => m.id);

  test('by the year the maker listed them, then by code with numbers read as numbers', () => {
    expect(ids([
      model('ipg-9', { listed_year: 2016 }), model('ipg-10', { listed_year: 2016 }),
      model('ivg-g5s', { listed_year: 2021 }), model('ahb', { listed_year: 2015 }),
    ])).toEqual(['ivg-g5s', 'ipg-9', 'ipg-10', 'ahb']);
  });

  test('an undated board takes the median year of the dated boards on its SoC; one with neither goes last', () => {
    expect(ids([
      model('nosoc'),
      model('old', { soc: 'hi3518ev100', listed_year: 2015 }),
      model('undated-old', { soc: 'hi3518ev100' }),
      model('new-a', { soc: 'gk7205v200', listed_year: 2020 }),
      model('new-b', { soc: 'gk7205v200', listed_year: 2022 }),
      model('new-c', { soc: 'gk7205v200', listed_year: 2021 }),
      model('undated-new', { soc: null, soc_label: 'GK7205V200' }),
    ])).toEqual(['new-b', 'new-c', 'undated-new', 'new-a', 'old', 'undated-old', 'nosoc']);
  });

  test("an undated board's SoC year comes from the whole catalogue, not the slice being ordered", () => {
    const dated = [model('other-maker', { soc: 'gk7205v300', listed_year: 2022 })];
    const slice = [model('old', { soc: 'hi3518ev100', listed_year: 2016 }), model('undated', { soc: 'gk7205v300' })];
    expect(ids(slice)).toEqual(['old', 'undated']);
    expect(newestFirst(slice, [...slice, ...dated]).map((m) => m.id)).toEqual(['undated', 'old']);
  });

  test('the layout lists each group newest first', () => {
    const file: BoardsFile = { ...FILE, manufacturers: [{ id: 'x', name: 'X', aliases: [], website: null, models: [
      model('a', { listed_year: 2015 }), model('b', { listed_year: 2023 }),
    ] }] };
    expect(layout(file.manufacturers, entries(file))[0].groups[0].entries.map((m) => m.id)).toEqual(['b', 'a']);
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
    expect(stats(ALL)).toEqual({ boards: 4, devices: 0, makers: 1, pinouts: 1, dumps: 3, needPinout: 3 });
    const withNvr = [...ALL, ...entries({ ...FILE, manufacturers: [{ id: 'xiongmai', name: 'Xiongmai', aliases: [], website: null,
      models: [model('nvr', { kind: 'recorder' })] }] })];
    expect(stats(withNvr)).toMatchObject({ boards: 4, devices: 1 });
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
    // With no code the name is the heading itself, not repeated below it.
    expect(s(sum('Anything'), null)).toBeNull();
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
      line: 'XVI&AHD Hybrid Camera Module', source: 'cctvsp', ready: true, kind: 'recorder' as const, model: 'xiongmai-ipg-50h20pls-s',
    };
    expect(writeQueryString(s)).toBe('?q=xm25qh64a&scope=all&maker=hsell&soc=hi3516cv300&sensor=IMX323&missing=pinout'
      + '&line=XVI%26AHD+Hybrid+Camera+Module&source=cctvsp&ready=1&kind=recorder&model=xiongmai-ipg-50h20pls-s');
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

describe("a seller's build", () => {
  test('says it adds the IPeye cloud when its file says so', () => {
    expect(addsIPeye({ key: 'c214__00001532.20170705__IPEYE_1532_IPC_HI3516C_53H20L.bin', build: 'IPEYE_1532_IPC_HI3516C_53H20L' })).toBe(true);
    expect(addsIPeye({ key: 'id1__000559A7.1__IPC_HI3516EV200.zip', build: 'IPC_HI3516EV200_50H20AI_S38' })).toBe(false);
  });
});

describe("sellers' builds on the page", () => {
  test('each seller is credited with its own builds', () => {
    const got = bySeller([{ origin: 'cctvsp.ru', k: 1 }, { origin: 'example.org', k: 2 }, { origin: 'cctvsp.ru', k: 3 }]);
    expect(got.map((g) => [g.origin, g.files.map((f) => f.k)])).toEqual([['cctvsp.ru', [1, 3]], ['example.org', [2]]]);
  });

  test("a seller's calendar date is the same day wherever the reader is", () => {
    const tz = process.env.TZ;
    process.env.TZ = 'America/Los_Angeles';
    try {
      expect(formatDay('2019-03-21T00:00:00Z', 'en-US', true)).toBe('Mar 21, 2019');
    } finally {
      process.env.TZ = tz;
    }
    expect(formatDay(null, 'en-US')).toBeNull();
    expect(formatDay('not a date', 'en-US')).toBeNull();
  });
});
