/**
 * Keeping the two sides of the flight A/B on the same frame.
 *
 * Two <video> elements with two DASH players and no shared clock: the drone's
 * side leads and the ground station's follows. This is the R&D Player's
 * method (OpenIPC/rnd-player, QualityCompare.tsx): a drift beyond a few frames
 * is a seek, anything smaller is nudged out by running the follower a few per
 * cent fast or slow, so it closes without the visible jump a seek costs.
 *
 * Pure, so the thresholds are tested rather than tuned by eye.
 */

/** Beyond this the follower seeks rather than catching up. */
export const SEEK_DRIFT = 0.2;
/**
 * Within this -- a quarter of a frame at 60 fps -- the two sides show the same
 * picture. Measured in Chrome, the follower settles at the edge of whatever
 * tolerance it is given: a whole frame left it one frame behind for good, half
 * a frame left it 8 ms behind, which is the previous frame for half of every
 * refresh.
 */
export const IN_STEP = 1 / 240;
/** How hard the follower leans while catching up. */
export const NUDGE = 0.03;

export type SyncAction =
  | { kind: 'seek'; to: number }
  | { kind: 'rate'; rate: number };

/**
 * What to do about the follower, given both clocks.
 *
 * `drift` is follower minus leader: positive means the ground station's side
 * shows a later moment than the drone's, so it is slowed.
 */
export function syncAction(leader: number, follower: number): SyncAction {
  const drift = follower - leader;
  if (Math.abs(drift) > SEEK_DRIFT) return { kind: 'seek', to: leader };
  if (Math.abs(drift) > IN_STEP) return { kind: 'rate', rate: drift > 0 ? 1 - NUDGE : 1 + NUDGE };
  return { kind: 'rate', rate: 1 };
}

/** The rendition a player is on, as far as the comparison cares. */
export interface Rung {
  height: number | null;
  bandwidth: number;
  videoCodec: string | null;
}

const isHevc = (codec: string | null) => !!codec && /^(hvc1|hev1)/.test(codec);

/**
 * The rendition that is the recording itself, not re-encoded.
 *
 * On the drone's side that is the highest bitrate. On the ground station's it
 * is the HEVC one, which is lower than its H.264 copy -- the copy exists for
 * browsers that cannot decode HEVC, and needs more bits to say the same thing.
 * Where the browser cannot play HEVC the player never lists it, and the best
 * this reader can see is the copy.
 */
export function originalOf<T extends Rung>(side: 'onboard' | 'gs', rungs: T[]): T | undefined {
  if (side === 'gs') {
    const hevc = rungs.find((r) => isHevc(r.videoCodec));
    if (hevc) return hevc;
  }
  return [...rungs].sort((a, b) => b.bandwidth - a.bandwidth)[0];
}

/** What the badge on one side says about the rendition it is showing. */
export type RungKind = 'original' | 'copy' | 'reduced';

export function rungKind(side: 'onboard' | 'gs', rung: Rung, all: Rung[]): RungKind {
  const original = originalOf(side, all);
  if (original && original.bandwidth === rung.bandwidth && original.videoCodec === rung.videoCodec) {
    return isHevc(rung.videoCodec) || side === 'onboard' ? 'original' : 'copy';
  }
  // The ground station's full-resolution H.264 rendition, on a browser that
  // can play the HEVC one, is still a stand-in for it rather than less of it.
  if (side === 'gs' && !isHevc(rung.videoCodec) && rung.height !== null && rung.height >= 1080) return 'copy';
  return 'reduced';
}

/** m:ss, from the start of the common window. */
export function clock(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
