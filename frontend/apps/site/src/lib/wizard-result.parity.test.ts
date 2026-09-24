/**
 * The settling, held to Rails' own answers (#164).
 *
 * `update` is the action that decides which commands a visitor is shown: it
 * moves a 16MB layout off an 8MB chip, an unpublished edition onto a published
 * one, and Ultimate off a 5120KB rootfs partition, and says so each time. The
 * static wizard has to reach the same answer -- a disagreement here is the two
 * halves of the site describing different installs of the same camera.
 *
 * The fixture is not written by hand. `bin/rails wizard:settled` makes a real
 * request per case and reads the settled configuration back out of the
 * permanent link the page prints -- which is `Camera#permalink` -- along with
 * the flash messages as rendered. So this compares against Rails' output, not
 * against a second statement of the rule.
 */
import { describe, expect, test } from 'vitest';
import fixture from './wizard-settled.fixture.json';
import { toPermalink } from './wizard-input';
import { flashArguments, fromForm, settle, type SocRules } from './wizard-result';
import { wizardTranslate } from './wizard-i18n';

const PATTERNS = {
  ip: '^((\\d{1,2}|1\\d\\d|2[0-4]\\d|25[0-5])\\.){3}(\\d{1,2}|1\\d\\d|2[0-4]\\d|25[0-5])$',
  mac: '^([a-fA-F\\d]{2}[:\\-]){5}[a-fA-F\\d]{2}$',
};

/** The form's own query shape, which is what `update` answers to. */
function query(asked: Record<string, string>): URLSearchParams {
  const params = new URLSearchParams();
  for (const [field, value] of Object.entries(asked)) params.set(`camera[${field}]`, value);
  return params;
}

/** `t("firmware.version.#{release}", default: release.capitalize)`. */
function versionName(release: string): string {
  return wizardTranslate('en', `firmware.version.${release}`, {
    fallback: release.charAt(0).toUpperCase() + release.slice(1),
  });
}

describe('what the wizard settles on', () => {
  for (const [urlname, soc] of Object.entries(fixture.socs)) {
    const rules: SocRules = {
      patterns: PATTERNS,
      editions: soc.editions,
      defaultFlashChip: soc.default_flash_chip,
      specialPages: soc.special_pages as Record<string, string | undefined>,
    };

    describe(urlname, () => {
      for (const one of soc.cases) {
        const asked = one.asked as unknown as Record<string, string>;

        test(JSON.stringify(asked), () => {
          const settled = settle(fromForm(query(asked), PATTERNS), rules);

          if ('page' in one) {
            // The fixture names the wiki page the template links to; the export
            // names the template. One combination, two names for it.
            expect(settled.page).toBe(
              one.page === 'fpv-sigmastar' ? 'sigmastar_nand' : 'hi3536dv100',
            );
            return;
          }

          expect(toPermalink(settled.settings)).toBe(one.permalink);

          const sentences = settled.flashes.map((message) => ({
            level: message.level,
            text: wizardTranslate('en', `cameras.socs.warnings.${message.key}`,
              flashArguments(message, versionName)),
          }));
          // Rails' flash keys are `warning` and `alert`; the classes the layout
          // draws them with are `warning` and `danger`.
          expect(sentences.map((s) => ({
            level: s.level === 'alert' ? 'danger' : 'warning',
            text: s.text,
          }))).toEqual(one.flashes);
        });
      }
    });
  }
});
