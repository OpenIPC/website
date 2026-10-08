/**
 * Every social link has a mark to draw (#402).
 *
 * The footer and /our-team look an icon up by name in a map of inlined SVGs,
 * and a name with no file behind it renders as an empty, invisible link
 * rather than failing the build. The marks also have to colour by
 * inheritance: the footer is dark and the team cards are light, and only
 * `fill="currentColor"` on a 24-unit Simple Icons box keeps them matching
 * their neighbours in both.
 */
import { describe, expect, test } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SOCIAL_LINKS } from './nav';
import { TEAM } from '../data/team';

const social = join(dirname(fileURLToPath(import.meta.url)), '..', 'assets', 'social');

const named = new Set([
  ...SOCIAL_LINKS.map((s) => s.icon),
  ...TEAM.flatMap((section) => section.members.flatMap((m) => m.socials.map((s) => s.icon))),
]);

describe('social icons', () => {
  test.each([...named])('%s has a mark that colours by inheritance', (icon) => {
    const file = join(social, `${icon}.svg`);
    expect(existsSync(file), `${icon}.svg is missing`).toBe(true);
    const svg = readFileSync(file, 'utf8');
    expect(svg).toContain('fill="currentColor"');
    expect(svg).toContain('viewBox="0 0 24 24"');
  });

  test('the official account is on Bluesky in the footer and on /our-team', () => {
    const url = 'https://bsky.app/profile/openipc.org';
    expect(SOCIAL_LINKS.some((s) => s.url === url && s.icon === 'bluesky')).toBe(true);
    const openipc = TEAM.flatMap((s) => s.members).find((m) => m.name === 'OpenIPC');
    expect(openipc?.socials.some((s) => s.url === url && s.icon === 'bluesky')).toBe(true);
  });
});
