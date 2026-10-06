import { describe, expect, it } from 'vitest';
import { clock, NUDGE, originalOf, rungKind, syncAction, type Rung } from './flight-sync';

describe('syncAction', () => {
  it('leaves two sides on the same frame alone', () => {
    expect(syncAction(10, 10)).toEqual({ kind: 'rate', rate: 1 });
    expect(syncAction(10, 10.003)).toEqual({ kind: 'rate', rate: 1 });
  });

  it('does not let the follower settle a whole or half a frame behind', () => {
    expect(syncAction(10, 10 - 1 / 60)).toEqual({ kind: 'rate', rate: 1 + NUDGE });
    expect(syncAction(10, 10 - 1 / 120)).toEqual({ kind: 'rate', rate: 1 + NUDGE });
  });

  it('slows a follower that runs ahead and hurries one that lags', () => {
    expect(syncAction(10, 10.05)).toEqual({ kind: 'rate', rate: 1 - NUDGE });
    expect(syncAction(10, 9.95)).toEqual({ kind: 'rate', rate: 1 + NUDGE });
  });

  it('seeks rather than chasing a large drift', () => {
    expect(syncAction(10, 10.5)).toEqual({ kind: 'seek', to: 10 });
    expect(syncAction(42, 3)).toEqual({ kind: 'seek', to: 42 });
  });
});

const onboard: Rung[] = [
  { height: 1080, bandwidth: 19_100_000, videoCodec: 'avc1.64002a' },
  { height: 1080, bandwidth: 8_000_000, videoCodec: 'avc1.64002a' },
  { height: 720, bandwidth: 4_000_000, videoCodec: 'avc1.640020' },
];
const gs: Rung[] = [
  { height: 1080, bandwidth: 7_800_000, videoCodec: 'hvc1.1.6.L120.90' },
  { height: 720, bandwidth: 3_000_000, videoCodec: 'hvc1.1.6.L93.90' },
];
const gsH264: Rung[] = [
  { height: 1080, bandwidth: 10_000_000, videoCodec: 'avc1.64002a' },
  { height: 720, bandwidth: 4_000_000, videoCodec: 'avc1.640020' },
];

describe('originalOf', () => {
  it("is the drone's highest bitrate", () => {
    expect(originalOf('onboard', onboard)).toBe(onboard[0]);
  });

  it("is the ground station's HEVC recording, not its larger H.264 copy", () => {
    expect(originalOf('gs', [...gsH264, ...gs])).toBe(gs[0]);
  });

  it('falls back to the copy where HEVC cannot be played', () => {
    expect(originalOf('gs', gsH264)).toBe(gsH264[0]);
  });
});

describe('rungKind', () => {
  it('names the recordings themselves', () => {
    expect(rungKind('onboard', onboard[0], onboard)).toBe('original');
    expect(rungKind('gs', gs[0], gs)).toBe('original');
  });

  it('says when the ground station is shown through its H.264 copy', () => {
    expect(rungKind('gs', gsH264[0], gsH264)).toBe('copy');
  });

  it('says when either side is below its recording', () => {
    expect(rungKind('onboard', onboard[1], onboard)).toBe('reduced');
    expect(rungKind('gs', gs[1], gs)).toBe('reduced');
    expect(rungKind('gs', gsH264[1], gsH264)).toBe('reduced');
  });
});

describe('clock', () => {
  it('reads as minutes and seconds', () => {
    expect(clock(0)).toBe('0:00');
    expect(clock(65.9)).toBe('1:05');
    expect(clock(360)).toBe('6:00');
  });
});
