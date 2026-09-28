/**
 * wall-decode is where a frame either becomes a picture or silently does not:
 * a decoder that fails leaves a tile looking exactly like a purged snapshot,
 * so every way out of it is pinned here rather than trusted.
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fit, hevcAnnexB, paintFrame, route, viaWebCodecs, type WallFrame } from './wall-decode';

afterEach(() => vi.unstubAllGlobals());

describe('fit', () => {
  it('fits inside the box, keeps the shape, and never enlarges', () => {
    expect(fit(3840, 2160, { width: 480, height: 360 })).toEqual({ width: 480, height: 270 });
    expect(fit(704, 576, { width: 480, height: 360 })).toEqual({ width: 440, height: 360 });
    expect(fit(320, 240, { width: 1920, height: 1080 })).toEqual({ width: 320, height: 240 });
    expect(fit(3840, 2160, { width: 1920, height: 1080 }, 2)).toEqual({ width: 3840, height: 2160 });
  });
});

describe('route', () => {
  it('names the decoder by codec string, and refuses what it does not know', () => {
    expect(route('avc1.4D0033')).toBe('avc');
    expect(route('hvc1.1.6.L153.B0')).toBe('hevc');
    expect(route('jpeg')).toBe('jpeg');
    expect(route('hev1.1.6.L93.B0')).toBeNull();
    expect(route('image/png')).toBeNull();
  });
});

/** An hvcC with one VPS, one SPS and one PPS, lengths of four bytes. */
function hvcC(): Uint8Array {
  const head = new Uint8Array(23);
  head[0] = 1;
  head[21] = 0x0f;
  head[22] = 3;
  const arrays = [[0x20, [0x40, 0x01, 0xaa]], [0x21, [0x42, 0x01, 0xbb, 0xbb]], [0x22, [0x44, 0x01]]] as const;
  const body: number[] = [];
  for (const [type, nal] of arrays) body.push(0x80 | type, 0, 1, 0, nal.length, ...nal);
  return Uint8Array.from([...head, ...body]);
}

describe('hevcAnnexB', () => {
  it('puts the parameter sets before the access unit, each behind a start code', () => {
    const au = Uint8Array.from([0, 0, 0, 3, 0x26, 0x01, 0xcc, 0, 0, 0, 2, 0x26, 0x01]);
    expect(Array.from(hevcAnnexB(hvcC(), au))).toEqual([
      0, 0, 0, 1, 0x40, 0x01, 0xaa,
      0, 0, 0, 1, 0x42, 0x01, 0xbb, 0xbb,
      0, 0, 0, 1, 0x44, 0x01,
      0, 0, 0, 1, 0x26, 0x01, 0xcc,
      0, 0, 0, 1, 0x26, 0x01,
    ]);
  });

  it('refuses a NAL that runs past the access unit', () => {
    expect(() => hevcAnnexB(hvcC(), Uint8Array.from([0, 0, 0, 9, 0x26, 0x01]))).toThrow();
  });
});

const frame: WallFrame = {
  codec: 'avc1.4D0033', width: 320, height: 240,
  description: Uint8Array.from([1, 0x4d, 0, 0x33, 0xff, 0xe1]), data: Uint8Array.from([0, 0, 0, 1, 0x65]),
};

/** A VideoDecoder that behaves as `script` says. */
function fakeDecoder(script: { output?: boolean; error?: boolean; configureThrows?: boolean; supported?: boolean }) {
  const closed: string[] = [];
  class Decoder {
    state = 'unconfigured';
    constructor(private init: { output: (f: unknown) => void; error: (e: unknown) => void }) {}
    static isConfigSupported = async () => ({ supported: script.supported ?? true });
    configure() {
      if (script.configureThrows) throw new Error('nope');
      this.state = 'configured';
    }
    decode() {}
    async flush() {
      if (script.output) this.init.output({ close: () => closed.push('frame'), displayWidth: 320, displayHeight: 240 });
      if (script.error) {
        this.init.error(new Error('decode failed'));
        throw new Error('flush aborted');
      }
    }
    close() { this.state = 'closed'; closed.push('decoder'); }
  }
  vi.stubGlobal('VideoDecoder', Decoder);
  vi.stubGlobal('EncodedVideoChunk', class { constructor(public init: unknown) {} });
  return closed;
}

describe('viaWebCodecs', () => {
  it('returns the one picture and closes the decoder', async () => {
    const closed = fakeDecoder({ output: true });
    const picture = await viaWebCodecs(frame);
    expect(picture).not.toBeNull();
    expect(closed).toEqual(['decoder']);
  });

  it('closes a picture that arrived before the decoder failed, and returns nothing', async () => {
    const closed = fakeDecoder({ output: true, error: true });
    expect(await viaWebCodecs(frame)).toBeNull();
    expect(closed.sort()).toEqual(['decoder', 'frame']);
  });

  it('returns nothing when the configuration is refused outright', async () => {
    fakeDecoder({ configureThrows: true });
    expect(await viaWebCodecs(frame)).toBeNull();
  });

  it('returns nothing when the flush completes with no picture', async () => {
    fakeDecoder({});
    expect(await viaWebCodecs(frame)).toBeNull();
  });
});

describe('paintFrame', () => {
  it('reports false, never throws, when this browser has no decoder at all', async () => {
    vi.stubGlobal('VideoDecoder', undefined);
    vi.stubGlobal('Worker', undefined);
    const canvas = {} as HTMLCanvasElement;
    expect(await paintFrame(canvas, frame, { width: 480, height: 360 })).toBe(false);
    expect(await paintFrame(canvas, { ...frame, codec: 'hvc1.1.6.L93.B0' }, { width: 480, height: 360 })).toBe(false);
    expect(await paintFrame(canvas, { ...frame, codec: 'image/png' }, { width: 480, height: 360 })).toBe(false);
    expect(await paintFrame(undefined, frame, { width: 480, height: 360 })).toBe(false);
  });
});
