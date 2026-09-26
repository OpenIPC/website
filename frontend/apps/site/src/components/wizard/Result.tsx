/**
 * The installation instructions.
 *
 * Every command on this page is rendered by the Go service and arrives as data (#163).
 * What is here is the page around them: which steps exist for this
 * combination, which bundle it links to, and the order a reader meets them in.
 * No offset, no partition size and no erase length is computed in this file,
 * and none should ever be.
 */
import { useRef, useState } from 'preact/hooks';
import type { ComponentChildren } from 'preact';
import type { Block, Combination, WizardDocument } from '../../lib/wizard-export';
import { fillHoles, type WizardSettings } from '../../lib/wizard-input';
import { toPermalink } from '../../lib/wizard-input';
import {
  flashArguments, flashFamily, stockBootloaderOnly, type FlashMessage,
} from '../../lib/wizard-result';
import SupportCount from '../SupportCount.tsx';
import Icon from './Icon.tsx';
import Terminal from './Terminal.tsx';
import { Breadcrumb, versionName, type SocFacts } from './Form.tsx';

type Translate = (key: string, options?: Record<string, unknown>) => string;

export interface SupportLabels {
  count: string; countMet: string; meter: string; monthly: string; split: string;
}

export interface ResultLinks {
  /** `locale_path('/business')`, `/donate`, `/open-wall`, `/community`. */
  business: string;
  donate: string;
  wall: string;
  community: string;
}

interface Props {
  t: Translate;
  facts: SocFacts & { segment: string };
  doc: WizardDocument;
  combination: Combination;
  settings: WizardSettings;
  flashes: FlashMessage[];
  links: ResultLinks;
  supportGoal: number;
  supportLabels: SupportLabels;
  locale: string;
  /** Where this SoC's wizard lives, for the permanent link. */
  here: string;
}

/** The three rooms, in CHAT_ROOMS' order. */
const CHAT_ROOMS: [key: string, url: string, label: string][] = [
  ['en', 'https://t.me/+7LL2kc32SOo5YWYy', 'OpenIPC Users (EN)'],
  ['fpv', 'https://t.me/+BMyMoolVOpkzNWUy', 'OpenIPC & FPV'],
  ['ru', 'https://t.me/+Sl2GPoR9G2iJAOCr', 'OpenIPC Users (RU)'],
];

/** A locale with no room of its own (zh) matches nothing and leaves the order alone. */
function moveFirst<T extends [string, ...unknown[]]>(rooms: T[], key: string): T[] {
  return [...rooms.filter((room) => room[0] === key), ...rooms.filter((room) => room[0] !== key)];
}

function Html({ as: Tag = 'p', class: className, html }:
{ as?: 'p' | 'div' | 'h2'; class?: string; html: string }) {
  return <Tag class={className} dangerouslySetInnerHTML={{ __html: html }} />;
}

/**
 * The notes a block has earned, with the one that assumes a full image
 * swapped where there is no full image to write.
 *
 * `mac_record_caveat_html` opens "the full image erases the whole chip", which
 * is the right reason on the guided path and a false one where the reader is
 * writing two files from their own bootloader. The advice -- write the address
 * down -- is the same either way.
 */
function noteFor(name: string, stockOnly: boolean): string {
  if (!stockOnly || name !== 'mac_record_caveat_html') return name;
  return 'mac_record_stock_caveat_html';
}

/** One command block: the terminal, and the notes the lines in it have earned. */
function Commands({ t, doc, combination, settings, name, stockOnly = false }:
{ t: Translate; doc: WizardDocument; combination: Combination; settings: WizardSettings;
  name: string; stockOnly?: boolean }) {
  const id = combination.blocks?.[name];
  if (id === undefined) return null;
  const block: Block | undefined = doc.blocks[id];
  if (!block) return null;

  // With a MAC given, four of the seven blocks change shape rather than just
  // their values: `setenv ethaddr` appears at all, and the address joins the
  // backup filename.
  const variant = settings.cameraMacAddress === '' ? undefined : combination.mac_variant?.[name];
  const lines = variant === undefined ? block.lines : doc.mac_variants[variant] ?? block.lines;

  return (
    <>
      <Terminal
        lines={fillHoles(lines, settings)}
        noPaste={block.no_paste}
        shellLabel={t('firmware.installation.shell_label')}
        doNotPaste={t('firmware.installation.do_not_paste')}
      />
      {block.notes.map((note) => (
        <Html
          key={note}
          class="text-[.875em] text-body-secondary"
          html={t(`firmware.installation.${noteFor(note, stockOnly)}`)}
        />
      ))}
    </>
  );
}

export default function Result({
  t, facts, doc, combination, settings, flashes, links, supportGoal, supportLabels, locale, here,
}: Props) {
  const update = (key: string, options?: Record<string, unknown>) =>
    t(`cameras.socs.update.${key}`, options);
  const install = (key: string, options?: Record<string, unknown>) =>
    t(`firmware.installation.${key}`, options);

  const edition = versionName(t, settings.firmwareVersion);
  const nand = settings.flashType === 'nand';

  /*
   * No OpenIPC bootloader for this chip, so most of this page does not apply.
   *
   * What survives is what a stock bootloader can do: the backup, which is
   * `sf probe`, `sf read` and `tftpput`, and the restore that undoes it. What
   * goes is everything that assumes OpenIPC's own bootloader -- the U-Boot
   * step, whose command comes out with a hole where the filename would be,
   * and the by-parts step, which is `run uknor8m; run urnor8m`, macros a stock
   * bootloader answers with `## Error: "uknor8m" not defined`.
   *
   * The congratulation goes too. These steps do not install anything; saying
   * they did would be the page's own copy contradicting what the reader just
   * ran.
   */
  const stockOnly = stockBootloaderOnly(doc);
  const sdcardRequired = settings.sdCardSlot === 'sd' && settings.networkInterface === 'wifi';

  // `post_flash_commands.any?`: the MAC line when there is one to set, plus
  // the layout macros when the bootloader does not already default to this
  // layout. Both facts come from the export; only "is there a MAC" is ours.
  const macCommand = settings.networkInterface !== 'wifi' && settings.cameraMacAddress !== '';
  const postFlash = macCommand || combination.default_bootloader_layout === false;

  const downloadHref = `${here}/download_full_image?flash_size=${combination.flash_size}`
    + `&flash_type=${combination.flash_family}&fw_release=${settings.firmwareVersion}`
    + `&layout=${combination.layout_size}`;

  const businessHref = `${links.business}?edition=${encodeURIComponent(settings.firmwareVersion)}`
    + `&ref=download-step&soc=${encodeURIComponent(doc.soc)}`;

  const [expertsOpen, setExpertsOpen] = useState(false);

  return (
    <>
      <Flashes t={t} flashes={flashes} />

      <header class="site-page-header py-6 lg:py-12">
        <div class="site-container">
          <Breadcrumb t={t} facts={facts} class="mb-4" />
          <div class="flex items-start justify-between gap-4">
            <div>
              <h1 class="mb-0">{install('title')} ({edition})</h1>
              <p class="site-lead mt-2 mb-0 text-body-secondary">
                {install('subtitle', {
                  soc_name: facts.fullName,
                  flash_type: t(`flash_chip.${settings.flashType}`),
                })}
              </p>
              {/*
                Which mtdparts these commands assume. Worth saying out loud: it
                is a separate choice from the chip now, and the erase below
                spans the chip whichever layout goes inside it.
              */}
              {/*
                Left off where there is no OpenIPC bootloader: the layout it
                names is that bootloader's mtdparts, and the image it says
                covers the whole chip is one this page cannot assemble.
              */}
              {!nand && !stockOnly && (
                <p class="mt-2 mb-0 text-[.875em] text-body-secondary">
                  {install('layout_note', {
                    layout: t(`flash_layout.${settings.partitionLayout}`),
                  })}
                </p>
              )}
            </div>
            {/*
              `shrink-0` on the anchor as well as on the image. Bootstrap's
              reboot leaves an <img> at its attribute width; Tailwind's
              preflight gives it `max-width: 100%`, so the badge shrank with
              the flex item around it and came out half size on a phone.
            */}
            <a class="shrink-0" href={facts.stagesHref} title={facts.statusTitle}>
              <img width="56" class="shrink-0" src={facts.stageSrc} alt="" />
            </a>
          </div>
          <p class="mt-4 mb-0 text-[.875em]">
            <a href={here + toPermalink(settings)}>{install('permanent_link')}</a>
          </p>
        </div>
      </header>

      <div class="site-container">
        <article class="firmware">
          <div class="site-alert site-alert-warning mt-12 flex gap-4">
            <Icon name="info-circle-fill" size="fs5" />
            <p class="mb-0">{update('files_download_alert')}</p>
          </div>

          {/*
            Step 1. Kept loudest on the page, because this is the step whose
            omission is unrecoverable -- but as a warning callout beside a
            normal section, not as a red box swallowing the commands.
          */}
          <section class="py-6">
            <h2 class="site-h4 mb-1">
              <span class="me-2 text-brand-blue">1</span>{install('backup.title')}
            </h2>
            <div class="site-alert site-alert-danger my-4 flex gap-4">
              <Icon name="exclamation-triangle-fill" size="fs5" />
              <p class="mb-0 font-semibold">{install('backup.subtitle')}</p>
            </div>
            <div class="site-row site-row-g4">
              <div class="site-col-lg-4">
                {/*
                  Why the backup matters is the same; what is about to
                  overwrite the camera is not. `backup.info` names OpenIPC's
                  U-Boot and the crypto partition it takes with it, and neither
                  is in this reader's path.
                */}
                <p class="text-body-secondary">
                  {install(stockOnly ? 'stock_backup_info' : 'backup.info')}
                </p>
              </div>
              <div class="site-col-lg-8">
                {sdcardRequired && (
                  <div class="site-alert site-alert-warning"><p class="mb-0">{update('sdcard_required_0')}</p></div>
                )}
                {nand
                  ? <p class="site-alert site-alert-warning">This part is currently under development. Stay tuned.</p>
                  : (
                    <>
                      <Commands
                        t={t} doc={doc} combination={combination} settings={settings}
                        name="firmware_backup" stockOnly={stockOnly}
                      />
                      {/*
                        Without a MAC the name carries nothing that tells one
                        camera from another, and the second backup of a batch
                        overwrites the first. Shown only when the name really is
                        shared.
                      */}
                      {settings.cameraMacAddress === '' && (
                        <p class="site-alert site-alert-warning">{install('backup.shared_name')}</p>
                      )}
                      {settings.flashType === 'nor32m' && (
                        <p class="site-alert site-alert-warning">{install('backup_32')}</p>
                      )}
                    </>
                  )}
              </div>
            </div>
            <Html
              class="mt-4 mb-0 text-body-secondary"
              html={install('backup.more_info_html')}
            />
          </section>

          {doc.instructable && (
            <section class="border-t border-hairline py-6">
              <h2 class="site-h4 mb-4">
                <span class="me-2 text-brand-blue">2</span>{install('flashing_full.title')}
              </h2>
              <div class="site-row site-row-g4">
                <div class="site-col-lg-4">
                  <div class="site-card h-full">
                    <div class="site-card-body">
                      <h3 class="site-h6 mb-2 flex items-start gap-2">
                        <Icon name="github" size="githubFs5" />
                        <a href={downloadHref}>
                          {install('flashing_full.link', {
                            name: titleize(settings.firmwareVersion),
                          })}
                        </a>
                      </h3>
                      <p class="text-[.875em] text-body-secondary">
                        for {facts.fullName} with {combination.flash_size}MB{' '}
                        {combination.flash_family?.toUpperCase()} flash
                        {!nand && combination.layout_size !== combination.flash_size
                          && `, ${combination.layout_size}MB partitions`}
                      </p>
                      <p class="mb-0 text-[.875em] text-body-secondary">{install('flashing_full.info')}</p>
                      {nand && (
                        <p class="mt-2 mb-0 text-[.875em] text-body-secondary">
                          {install('flashing_full.nand_caveat')}
                        </p>
                      )}
                    </div>
                  </div>
                </div>
                <div class="site-col-lg-8">
                  {sdcardRequired && (
                    <div class="site-alert site-alert-warning"><p class="mb-0">{update('sdcard_required_1')}</p></div>
                  )}
                  <Commands t={t} doc={doc} combination={combination} settings={settings} name="flashing_everything" />
                  <p>{install('flashing_full.continue')}</p>
                  {/*
                    A full-image flash leaves the env erased, so everything the
                    bootloader needs is set at this prompt: the MAC address,
                    which is otherwise never set at all on this path, and the
                    layout, where the bootloader does not already default to it.
                  */}
                  {postFlash && (
                    <>
                      <p>{install('flashing_full.continue2')}</p>
                      {macCommand && <Html html={install('flashing_full.mac_note_html')} />}
                      {combination.default_bootloader_layout === false && (
                        <p>{install('flashing_footfs.info')}</p>
                      )}
                      <Commands
                        t={t} doc={doc} combination={combination} settings={settings}
                        name="post_flash_environment"
                      />
                    </>
                  )}
                </div>
              </div>
              {/* Full width under both columns, after everything that gets
                  pasted into a bootloader and never among it (#190). */}
              <LicenceNotice t={t} facts={facts} businessHref={businessHref} />
            </section>
          )}

          {stockOnly && (
            <StockBootloader
              t={t} doc={doc} facts={facts} settings={settings} combination={combination}
            />
          )}

          {!stockOnly && (
          <div class="site-alert site-alert-success my-6 flex gap-4">
            <Icon name="check-circle-fill" size="fs4" />
            <div>
              <h2 class="site-h5 mb-1">{install('success.title', { name: edition })}</h2>
              <Html class="mb-0" html={install('success.info_html', { address: settings.cameraIpAddress })} />
              <WhatNext
                t={t} locale={locale} segment={facts.segment} links={links} businessHref={businessHref}
              />
              {/* The same number the donate page prints (#198). */}
              <SupportCount goal={supportGoal} labels={supportLabels} class="mt-4" />
            </div>
          </div>
          )}

          {/*
            The by-parts path, and the link that opens it -- in that order,
            because that is the order the page has: the block sits collapsed
            above its own link, with the printenv hint between them.

            Every part of it is OpenIPC's bootloader's, so a chip without one
            gets the restore on its own instead; see StockBootloader.
          */}
          {!stockOnly && (
            <>
              <Collapse id="collapseExperts" open={expertsOpen}>
                <Experts
                  t={t} doc={doc} combination={combination} settings={settings} facts={facts}
                  sdcardRequired={sdcardRequired} edition={edition}
                />
              </Collapse>

              <Html
                html={t('firmware.info_html', {
                  commands: (combination.bootloader_variables ?? [])
                    .map((name) => `<code>${name}</code>`).join(', '),
                })}
              />

              <a
                href="#collapseExperts"
                role="button"
                aria-expanded={expertsOpen ? 'true' : 'false'}
                aria-controls="collapseExperts"
                onClick={(event) => { event.preventDefault(); setExpertsOpen(!expertsOpen); }}
              >{update('advanced_instruction_link')}</a>
            </>
          )}
        </article>
      </div>

      <section class="bg-ink py-section text-white/85 mt-section">
        <div class="site-container text-center">
          <h2 class="mb-6 text-h2">{install('help_title')}</h2>
          <p class="mb-0 flex flex-wrap justify-center gap-2">
            <a
              class="site-btn site-btn-lg site-btn-primary"
              href={links.community}
              data-event="wizard:help"
            >{install('help_cta')}</a>
          </p>
        </div>
      </section>
    </>
  );
}

/** `String#titleize` on one word, which is all this is ever given. */
function titleize(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/**
 * `display_flashes`, above everything, which is where the layout puts them.
 *
 * `alert` is the red key and `warning` the amber one; the classes are
 * FLASH_CLASSES', not the key names.
 */
function Flashes({ t, flashes }: { t: Translate; flashes: FlashMessage[] }) {
  if (flashes.length === 0) return null;

  return (
    <div class="site-container">
      <div>
        {flashes.map((message) => (
          <div
            key={message.key}
            class={`mt-6 site-alert site-alert-${message.level === 'alert' ? 'danger' : 'warning'}`}
            role="alert"
          >
            {t(`cameras.socs.warnings.${message.key}`,
              flashArguments(message, (release) => versionName(t, release)))}
          </div>
        ))}
      </div>
    </div>
  );
}

/**
 * `download_licence_notice` (#190): what the image may be used for, under the
 * step that produces it and never among the commands.
 */
function LicenceNotice({ t, facts, businessHref }: {
  t: Translate; facts: SocFacts & { segment: string }; businessHref: string;
}) {
  if (facts.status !== 'done') return null;

  const install = (key: string) => t(`firmware.installation.${key}`);

  return (
    <div class="download-licence mt-2 border-t border-hairline pt-4 text-[.875em] text-body-secondary">
      <Html class="mb-0" html={install('licence_html')} />
      {/*
        A question, not an offer: the visitor decides whether it is about them.
        `?ref=` carries the attribution even when the beacon does not, since a
        same-tab navigation can cancel an async request.
      */}
      {facts.segment !== 'consumer' && (
        <p class="mb-0">
          {t(`firmware.installation.licence_ask.${facts.segment}`)}{' '}
          <a
            href={businessHref}
            data-event={`download-step:business:${facts.segment}`}
            data-volume-text={install('licence_volume_ask')}
            data-volume-link={install('licence_volume_link')}
          >{install('licence_business_link')}</a>.
        </p>
      )}
    </div>
  );
}

/**
 * What to do once the camera is up (#191).
 *
 * Every visitor is offered every door; the locale and the chip decide the
 * order, and nothing else. Routing on the locale was the deeper mistake the
 * rule this replaces made: a language setting is not a network, so ordering on
 * it is honest and removing a door on it strands the Chinese-reading FPV
 * builder whose VPN works.
 */
function WhatNext({ t, locale, segment, links, businessHref }: {
  t: Translate; locale: string; segment: string; links: ResultLinks; businessHref: string;
}) {
  const install = (key: string) => t(`firmware.installation.${key}`);

  let rooms = moveFirst(CHAT_ROOMS, locale);
  if (segment === 'fpv') rooms = moveFirst(rooms, 'fpv');

  const doors = (
    <ul class="ms-4 mt-1 mb-0 list-none ps-0 text-[.875em]" data-chat="true">
      {rooms.map(([key, url, label]) => (
        <li key={key}>
          <a href={url} data-event={`download-step:chat:${key}`}>{label}</a>
          {' — '}{t(`pages.community.channel_${key}`)}
        </li>
      ))}
    </ul>
  );
  const wayout = (
    <Html
      class="ms-4 mt-1 mb-0 text-[.875em] text-body-secondary"
      html={install('chat_blocked_html')}
    />
  );

  const support = ['fpv', 'cctv'].includes(segment)
    ? { key: 'business', href: businessHref, event: `download-step:business:${segment}` }
    : { key: 'donate', href: links.donate, event: 'download-step:donate' };

  return (
    <ul class="mt-4 mb-0 list-none ps-0" data-whatnext="true">
      <li class="mb-1">
        {install('chat_lede')}
        {/* Chinese has no room of its own, so the way out is the first useful
            thing on the list rather than a footnote under three links. */}
        {locale === 'zh' ? <>{wayout}{doors}</> : <>{doors}{wayout}</>}
      </li>
      <li class="mb-1">
        <a href={links.wall} data-event="download-step:wall">{install('next_wall_link')}</a>{' '}
        {install('next_wall_text')}
      </li>
      <li class="mb-1">
        <a href={support.href} data-event={support.event}>{install(`next_${support.key}_link`)}</a>{' '}
        {install(`next_${support.key}_text`)}
      </li>
    </ul>
  );
}

/**
 * The by-parts path, behind the link at the foot of the page.
 *
 * Bootstrap animates the height and this does the same, because the section is
 * most of the page: snapping it open moves everything the reader was looking
 * at.
 */
function Experts({ t, doc, combination, settings, facts, sdcardRequired, edition }: {
  t: Translate; doc: WizardDocument; combination: Combination; settings: WizardSettings;
  facts: SocFacts; sdcardRequired: boolean; edition: string;
}) {
  const update = (key: string) => t(`cameras.socs.update.${key}`);
  const install = (key: string, options?: Record<string, unknown>) =>
    t(`firmware.installation.${key}`, options);
  const nand = settings.flashType === 'nand';

  return (
    <>
      <h3>{install('flashing.alternative')}</h3>

          <div class="site-alert site-alert-secondary">
            <h3 class="mb-6 font-bold">{install('flashing_uboot.title')}</h3>
            <div class="site-row">
              <div class="site-col site-col-lg-4">
                {doc.uboot_filename !== '' && (
                  <div class="github">
                    <Icon name="github" size="github" class="float-start me-2" />
                    <h6 class="site-h6 mb-0">
                      <a href={doc.bl_url} title={doc.uboot_filename}>{install('flashing_uboot.link')}</a>
                    </h6>
                    <p>for {facts.fullName}</p>
                    <p class="mb-0">{install('flashing_uboot.info')}</p>
                  </div>
                )}
              </div>
              <div class="site-col site-col-lg-8">
                {sdcardRequired && (
                  <div class="site-alert site-alert-warning"><p class="mb-0">{update('sdcard_required_2')}</p></div>
                )}
                <Commands t={t} doc={doc} combination={combination} settings={settings} name="flashing_uboot" />
                <p>{install('flashing_uboot.continue')}</p>
              </div>
            </div>

            {/*
              Skipped where there is nothing to run. This step used to render
              unconditionally, so a SigmaStar or Ingenic reader was given `run
              setnor8m` -- a variable their bootloader does not define -- under
              a heading telling them it was required.
            */}
            {combination.layout_commands && (
              <>
                <h3 class="mb-6 font-bold">{install('flashing_footfs.title')}</h3>
                <div class="site-row">
                  <div class="site-col site-col-lg-4"></div>
                  <div class="site-col site-col-lg-8">
                    <p>{install('flashing_footfs.info')}</p>
                    <Commands
                      t={t} doc={doc} combination={combination} settings={settings}
                      name="preparing_environment"
                    />
                    <p>{install('flashing_footfs.continue')}</p>
                  </div>
                </div>
              </>
            )}

            <h3 class="mb-6 font-bold">{install('flashing_footfs2.title')}</h3>
            <div class="site-row">
              <div class="site-col site-col-lg-4">
                {doc.linux_filename !== '' && (
                  <div class="github">
                    <Icon name="github" size="github" class="float-start me-2" />
                    <h6 class="site-h6 mb-0">
                      <a href={combination.firmware_url} title={combination.firmware_filename}>
                        {install('flashing_footfs2.link', { name: edition })}
                      </a>
                    </h6>
                    <p>for {facts.fullName}</p>
                    <p class="mb-0">{install('flashing_footfs2.info')}</p>
                  </div>
                )}
              </div>
              <div class="site-col site-col-lg-8">
                {sdcardRequired && (
                  <div class="site-alert site-alert-warning"><p class="mb-0">{update('sdcard_required_3')}</p></div>
                )}
                <Commands t={t} doc={doc} combination={combination} settings={settings} name="flashing_linux" />
              </div>
            </div>
          </div>

      {!nand && (
        <div class="site-alert site-alert-warning">
          <h3 class="mb-6 font-bold">{t('firmware.restore.title')}</h3>
          <div class="site-row">
            <div class="site-col site-col-lg-4">
              <p>{t('firmware.restore.info')}</p>
            </div>
            <div class="site-col site-col-lg-8">
              <Commands
                t={t} doc={doc} combination={combination} settings={settings}
                name="restore_from_backup"
              />
            </div>
          </div>
        </div>
      )}
    </>
  );
}

/**
 * What is left of the installation when there is no OpenIPC bootloader (#164).
 *
 * The bundle, what is inside it, and the one thing this site will not do:
 * compose the two commands that write the kernel and the root filesystem.
 * Their offsets are a property of the bootloader on the board rather than of
 * the chip -- OpenIPC's own arrive with OpenIPC's bootloader, which this SoC
 * does not have -- and a wrong offset at the start of flash is the bootloader.
 * `guarded_flash` carries the report of a camera lost that way.
 *
 * So the page says that, names the two files, and points at the wiki and the
 * chat, which is where a layout gets worked out with somebody who has the
 * board. The restore block above it is real and stays: it is the backup's
 * counterpart and needs no macro.
 */
function StockBootloader({ t, doc, facts, settings, combination }: {
  t: Translate; doc: WizardDocument; facts: SocFacts; settings: WizardSettings;
  combination: Combination;
}) {
  const install = (key: string, options?: Record<string, unknown>) =>
    t(`firmware.installation.${key}`, options);
  /*
   * The bundle for this edition AND this chip.
   *
   * Matching on the edition alone was wrong: ten SoCs publish `ultimate` for
   * both NOR and NAND, so the first entry could hand a NAND visitor the NOR
   * filename and the NOR URL. None of them is bootloader-less today, which is
   * the only reason it was not already visible -- found by review on #276.
   */
  const family = flashFamily(settings.flashType);
  const bundle = doc.published.find(
    (one) => one.release === settings.firmwareVersion && one.flash_type === family,
  ) ?? doc.published.find((one) => one.flash_type === family);

  return (
    <>
      <section class="border-t border-hairline py-6">
        <h2 class="site-h4 mb-4">
          <span class="me-2 text-brand-blue">2</span>{install('stock_bundle_title')}
        </h2>
        <div class="site-row site-row-g4">
          <div class="site-col-lg-4">
            {bundle && (
              <div class="site-card h-full">
                <div class="site-card-body">
                  <h3 class="site-h6 mb-2 flex items-start gap-2">
                    <Icon name="github" size="githubFs5" />
                    <a href={bundle.url} title={bundle.filename}>
                      {t('cameras.socs.show.bundle', {
                        name: `${versionName(t, bundle.release)} ${bundle.flash_type.toUpperCase()}`,
                      })}
                    </a>
                  </h3>
                  <p class="mb-0 text-[.875em] text-body-secondary">
                    {t('cameras.socs.show.for', { name: facts.fullName })}
                  </p>
                </div>
              </div>
            )}
          </div>
          <div class="site-col-lg-8">
            <Html
              html={install('stock_bundle_info_html', {
                kernel: doc.kernel_file, rootfs: doc.rootfs_file,
              })}
            />
          </div>
        </div>
      </section>

      <section class="border-t border-hairline py-6">
        <h2 class="site-h4 mb-4">
          <span class="me-2 text-brand-blue">3</span>{install('stock_title')}
        </h2>
        <div class="site-alert site-alert-warning">
          <Html class="mb-0" html={install('stock_info_html')} />
        </div>
      </section>

      {/*
        The backup's counterpart, and the reason step 1 is worth the trouble.
        It writes the whole chip back from the file, needs no macro, and is
        the way out of a write that went somewhere it should not have.
      */}
      {settings.flashType !== 'nand' && (
        <section class="border-t border-hairline py-6">
          <h2 class="site-h4 mb-4">
            <span class="me-2 text-brand-blue">4</span>{t('firmware.restore.title')}
          </h2>
          <div class="site-row site-row-g4">
            <div class="site-col-lg-4">
              <p class="text-body-secondary">{t('firmware.restore.info')}</p>
            </div>
            <div class="site-col-lg-8">
              <Commands
                t={t} doc={doc} combination={combination} settings={settings}
                name="restore_from_backup"
              />
            </div>
          </div>
        </section>
      )}
    </>
  );
}

/**
 * Bootstrap's collapse, height and all.
 *
 * The animation is not decoration here: the block is most of the page, so
 * snapping it open moves everything the reader was looking at. It ends on
 * `auto` rather than the measured height, so a block that reflows -- a
 * translation that wraps differently, a window that narrows -- is not stuck at
 * yesterday's height.
 */
function Collapse({ id, open, children }:
{ id: string; open: boolean; children: ComponentChildren }) {
  const element = useRef<HTMLDivElement>(null);
  const was = useRef(open);

  // Ordinary DOM writes rather than state, because what is animated is a
  // measurement of the element itself and a render cannot know it.
  if (element.current && was.current !== open) {
    const node = element.current;
    was.current = open;
    const height = node.scrollHeight;
    node.style.height = open ? '0px' : `${height}px`;
    node.removeAttribute('data-open');
    requestAnimationFrame(() => {
      node.style.height = open ? `${height}px` : '0px';
    });
    node.addEventListener('transitionend', function done() {
      node.removeEventListener('transitionend', done);
      node.style.height = '';
      if (open) node.setAttribute('data-open', 'yes');
    });
  }

  return (
    <div id={id} class="site-collapse" ref={element} data-open={open ? 'yes' : undefined}>
      {children}
    </div>
  );
}
