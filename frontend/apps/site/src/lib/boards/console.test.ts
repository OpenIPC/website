/**
 * The console viewer's pure half: a capture replayed as the terminal showed
 * it, the highlighting of lines that carry no colour, and the facts and
 * flash partitions read off a U-Boot console.
 */
import { describe, expect, test } from 'vitest';
import { consoleFacts, highlight, humanBytes, lineText, markRuns, parseMtdparts, terminal } from './console';

describe('the terminal', () => {
  test('plain text is lines, unstyled', () => {
    const s = terminal('U-Boot 2010.06\n\nDRAM:  256 MiB\n');
    expect(s.styled).toBe(false);
    expect(s.lines.map(lineText)).toEqual(['U-Boot 2010.06', '', 'DRAM:  256 MiB']);
  });

  test('a last line without a newline is kept, CRLF is one line break', () => {
    expect(terminal('a\r\nb').lines.map(lineText)).toEqual(['a', 'b']);
  });

  test('colour and weight come through as the camera printed them', () => {
    const s = terminal('\x1b[1;32mOK\x1b[0m done \x1b[31mfail\x1b[m\n');
    expect(s.styled).toBe(true);
    expect(s.lines[0]).toEqual([
      { text: 'OK', style: { bold: true, fg: '#98c379' } },
      { text: ' done ' },
      { text: 'fail', style: { fg: '#e06c75' } },
    ]);
  });

  test('256 colours and 24-bit colour', () => {
    const [seg256] = terminal('\x1b[38;5;196mx').lines[0];
    expect(seg256.style?.fg).toBe('rgb(255,0,0)');
    const [segRgb] = terminal('\x1b[48;2;1;2;3my').lines[0];
    expect(segRgb.style?.bg).toBe('rgb(1,2,3)');
  });

  test('a carriage return rewrites the line, as a progress counter does', () => {
    expect(terminal('Erasing  10%\rErasing  55%\rErasing 100%\n').lines.map(lineText)).toEqual(['Erasing 100%']);
    // A shorter rewrite leaves the tail of the longer line, as on a real terminal...
    expect(terminal('abcdef\rXY\n').lines.map(lineText)).toEqual(['XYcdef']);
    // ...unless the program erases it.
    expect(terminal('abcdef\rXY\x1b[K\n').lines.map(lineText)).toEqual(['XY']);
  });

  test('a control sequence with intermediate bytes is dropped whole', () => {
    expect(terminal('a\x1b[!pb\x1b[0 qc\n').lines.map(lineText)).toEqual(['abc']);
  });

  test('inverse is an attribute: SGR 27 turns it off, a second SGR 7 does not turn it back', () => {
    const [on, off] = terminal('\x1b[31;7mA\x1b[27mB').lines[0];
    expect(on.style).toEqual({ fg: '#e06c75', inverse: true });
    expect(off.style).toEqual({ fg: '#e06c75' });
    const [twice] = terminal('\x1b[7m\x1b[7mC').lines[0];
    expect(twice.style).toEqual({ inverse: true });
  });

  test('a large plain file is read quickly, without a cell per character', () => {
    const big = Array.from({ length: 100_000 }, (_, i) => `[ ${i}.000000] line ${i}`).join('\n');
    const t0 = performance.now();
    const s = terminal(big);
    expect(s.lines).toHaveLength(100_000);
    expect(performance.now() - t0).toBeLessThan(1000);
  });

  test('a backspace steps back; other escapes and control bytes are not text', () => {
    expect(terminal('ab\bc\n').lines.map(lineText)).toEqual(['ac']);
    expect(terminal('\x1b]0;title\x07\x1b[2J\x1b[Hhello\x07\n').lines.map(lineText)).toEqual(['hello']);
  });
});

describe('highlighting a line with no colour', () => {
  const kinds = (line: string) => highlight(line).filter((s) => s.token).map((s) => [s.token, s.text]);

  test('the U-Boot prompt and the command typed after it', () => {
    expect(kinds('hisilicon # printenv')).toEqual([['prompt', 'hisilicon # '], ['cmd', 'printenv']]);
    expect(kinds('/ # cat /proc/mtd')).toEqual([['prompt', '/ # '], ['cmd', 'cat /proc/mtd']]);
  });

  test('a variable, hex numbers and sizes', () => {
    expect(kinds('bootcmd=sf probe 0;sf read 0x82000000 0x50000 0x300000;bootm 0x82000000')).toEqual([
      ['key', 'bootcmd'], ['hex', '0x82000000'], ['hex', '0x50000'], ['hex', '0x300000'], ['hex', '0x82000000'],
    ]);
    expect(kinds('mtdparts=hi_sfc:256K(boot),3520K(romfs)')).toEqual([['key', 'mtdparts'], ['size', '256K'], ['size', '3520K']]);
  });

  test('kernel timestamps, errors, warnings and the stages of a boot', () => {
    expect(kinds('[    1.234567] mmc0: error -110 whilst initialising')).toEqual([['time', '[    1.234567]'], ['error', 'error']]);
    expect(kinds('WARNING: bad CRC, using default environment')).toEqual([['warn', 'WARNING'], ['error', 'bad']]);
    expect(kinds('Starting kernel ...')).toEqual([['mark', 'Starting kernel ...']]);
  });

  test('every character is kept, highlighted or not', () => {
    const line = 'Spi(cs1): Block:64KB Chip:8MB Name:"MX25L6406E"';
    expect(lineText(highlight(line))).toBe(line);
  });
});

describe('the flash partitions', () => {
  test('sizes, offsets and the rest of the chip', () => {
    expect(parseMtdparts('hi_sfc:256K(boot),3520K(romfs),2560K(user),-(rest)')).toEqual([{
      device: 'hi_sfc',
      parts: [
        { name: 'boot', size: 256 * 1024, offset: 0 },
        { name: 'romfs', size: 3520 * 1024, offset: 256 * 1024 },
        { name: 'user', size: 2560 * 1024, offset: 3776 * 1024 },
        { name: 'rest', size: null, offset: 6336 * 1024 },
      ],
    }]);
  });

  test('hex sizes, an explicit offset, read-only, two devices', () => {
    expect(parseMtdparts('sfc:0x40000(uboot)ro,1M@0x80000(kernel);nand:-(ubi)')).toEqual([
      { device: 'sfc', parts: [{ name: 'uboot', size: 0x40000, offset: 0, readonly: true }, { name: 'kernel', size: 1024 ** 2, offset: 0x80000 }] },
      { device: 'nand', parts: [{ name: 'ubi', size: null, offset: 0 }] },
    ]);
  });

  test('what cannot be read is left out', () => {
    expect(parseMtdparts('nonsense')).toEqual([]);
    expect(parseMtdparts('sfc:lots(boot)')).toEqual([]);
  });
});

describe('the facts of a console', () => {
  const log = [
    'U-Boot 2010.06-svn (Oct 14 2015 - 15:05:28)',
    '',
    'DRAM:  256 MiB',
    'Spi(cs1): Block:64KB Chip:8MB Name:"MX25L6406E"',
    'hisilicon # printenv',
    'bootargs=mem=${osmem} console=ttyAMA0,115200 root=/dev/mtdblock1 rootfstype=cramfs mtdparts=hi_sfc:256K(boot),3520K(romfs),-(rest)',
    '[    0.000000] Linux version 3.0.8 (root@build) (gcc version 4.4.1) #1 Tue Oct 13',
    'Machine: hi3518',
  ].join('\n');

  test('U-Boot, DRAM, the flash chip and its size, the kernel and the machine', () => {
    const f = consoleFacts(log);
    expect(f).toMatchObject({
      uboot: '2010.06-svn (Oct 14 2015 - 15:05:28)', dram: '256 MiB', flash: 'MX25L6406E', flashBytes: 8 * 1024 ** 2,
      kernel: '3.0.8', machine: 'hi3518',
    });
    expect(f.bootargs).toMatch(/^mem=\$\{osmem\} console=ttyAMA0/);
    expect(f.mtd?.[0].parts.map((p) => p.name)).toEqual(['boot', 'romfs', 'rest']);
  });

  test('a fact rewritten on its line by a carriage return is read as it was left', () => {
    expect(consoleFacts('DRAM:  128 MiB\rDRAM:  256 MiB\n').dram).toBe('256 MiB');
  });

  test('a log that says none of it has no facts', () => {
    expect(consoleFacts('hello\nworld\n')).toEqual({});
  });

  test('sizes in the units mtdparts writes them in', () => {
    expect([humanBytes(256 * 1024), humanBytes(3520 * 1024), humanBytes(8 * 1024 ** 2), humanBytes(1536 * 1024), humanBytes(1500)])
      .toEqual(['256 KB', '3520 KB', '8 MB', '1536 KB', '1.5 KB']);
  });
});

describe('marking a search in a line', () => {
  test('a match across a token boundary is marked, and each piece keeps its token', () => {
    const runs = highlight('hisilicon # printenv');
    const got = markRuns(runs, '# print').filter((r) => r.mark);
    expect(got.map((r) => [r.text, r.token])).toEqual([['# ', 'prompt'], ['print', 'cmd']]);
  });

  test('a match inside one run splits it; no query or a short one marks nothing', () => {
    const got = markRuns([{ text: 'bootcmd=sf probe' }], 'cmd=sf');
    expect(got).toEqual([{ text: 'boot' }, { text: 'cmd=sf', mark: true }, { text: ' probe' }]);
    expect(markRuns([{ text: 'abc' }], '')).toEqual([{ text: 'abc' }]);
    expect(markRuns([{ text: 'abc' }], 'ab')).toEqual([{ text: 'abc' }]);
  });
});
