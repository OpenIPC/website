/**
 * app/views/cameras/socs/show.html.erb -- the wizard's form, and what stands
 * in its place for a chip nothing has been published for.
 *
 * Which of the two a visitor sees is a question about the release index, not
 * about the catalogue, so it is decided here rather than at build time. The
 * page is prerendered with the form, because that is what 105 of the 126 SoCs
 * show, and corrects itself when the export lands.
 */
import type { WizardDocument } from '../../lib/wizard-export';
import type { WizardSettings } from '../../lib/wizard-input';
import { FLASH_CHIPS, PARTITION_LAYOUTS, type Narrowed } from '../../lib/wizard-menu';
import Icon from './Icon.tsx';

export interface SocFacts {
  fullName: string;
  model: string;
  status: string;
  statusTitle: string;
  stageSrc: string;
  vendorName: string;
  vendorHref: string;
  socHref: string;
  vendorsHref: string;
  homeHref: string;
  stagesHref: string;
}

interface Props {
  t: (key: string, options?: Record<string, unknown>) => string;
  facts: SocFacts;
  doc: WizardDocument | null;
  settings: WizardSettings;
  narrowed: Narrowed | null;
  onChange: (field: keyof WizardSettings, value: string) => void;
  onGenerateMac: () => void;
  onSubmit: () => void;
  /** Said out loud rather than logged: the form cannot be trusted without it. */
  failed: boolean;
}

/** The wizard's breadcrumb, which both of its pages carry. */
export function Breadcrumb({ t, facts, class: className = '' }:
{ t: Props['t']; facts: SocFacts; class?: string }) {
  return (
    <nav aria-label="breadcrumb">
      <ol class={`site-breadcrumb ${className}`}>
        <li><a href={facts.homeHref}>{t('nav.home')}</a></li>
        <li><a href={facts.vendorsHref}>{t('nav.vendors')}</a></li>
        <li><a href={facts.vendorHref}>{facts.vendorName}</a></li>
        <li aria-current="page"><a href={facts.socHref}>{facts.model}</a></li>
      </ol>
    </nav>
  );
}

/** `t("firmware.version.#{release}", default: release.capitalize)`. */
export function versionName(t: Props['t'], release: string): string {
  return t(`firmware.version.${release}`, {
    fallback: release.charAt(0).toUpperCase() + release.slice(1),
  });
}

export default function Form({
  t, facts, doc, settings, narrowed, onChange, onGenerateMac, onSubmit, failed,
}: Props) {
  const show = (key: string, options?: Record<string, unknown>) =>
    t(`cameras.socs.show.${key}`, options);

  // The gate `show` applies, asked of the release index rather than of a
  // filename: a SoC can name a bootloader upstream does not publish, and the
  // blank check let it through to a form offering to build an image around a
  // bootloader that does not exist.
  const usable = doc === null
    || (doc.bootloader_published && doc.linux_filename !== ''
      && doc.load_address !== '' && doc.availability !== 'none');

  return (
    // `<main><div class="container mb-4">`, which is what the layout wraps a
    // page in unless it asks for the full width. The result page does ask, and
    // carries its own containers; this one does not.
    <div class="wizard-body site-container mb-6">
      <Breadcrumb t={t} facts={facts} />

      <article class="sensor">
        <header>
          <a class="float-end" href={facts.stagesHref} title={facts.statusTitle}>
            <img width="64" src={facts.stageSrc} alt="" />
          </a>
          <h2 class="site-display-3 mt-4 mb-6">{facts.fullName}</h2>
        </header>

        <div class="site-row">
          {usable
            ? (
              <>
                <div class="site-col-md-6 site-col-xl-5 site-col-xxl-4 mb-4">
                  <div class="site-alert site-alert-info">
                    <h3>{show('title')}</h3>
                    <p class="mb-0">{show('paragraph1')}</p>
                  </div>
                  <div class="site-alert site-alert-danger">
                    <h3>{show('title2')}</h3>
                    <p class="mb-0">{show('paragraph2')}</p>
                  </div>
                </div>

                <div class="site-col-md-6 site-col-xl-7 site-col-xxl-8 mb-4">
                  {failed && (
                    <div class="site-alert site-alert-warning" role="alert">
                      <p class="mb-0">{show('export_unreachable')}</p>
                    </div>
                  )}
                  <form
                    id="new_camera"
                    onSubmit={(event) => { event.preventDefault(); onSubmit(); }}
                  >
                    <div class="site-row site-row-cols-1 site-row-cols-xl-2">
                      <div class="site-col">
                        <div class="mb-4">
                          {/*
                            Not required: the camera gives itself a stable
                            unique address on first boot, so this is for keeping
                            an address a camera already has, not for inventing
                            one. An empty value skips `pattern` too -- HTML only
                            applies it to a non-empty field.
                          */}
                          <label class="site-form-label" for="camera_camera_mac_address">
                            {t('activemodel.attributes.camera.camera_mac_address')}
                          </label>
                          <input
                            class="site-form-control"
                            id="camera_camera_mac_address"
                            name="camera[camera_mac_address]"
                            type="text"
                            pattern={doc?.patterns.mac}
                            title={show('camera_mac_address_title')}
                            value={settings.cameraMacAddress}
                            onInput={(event) =>
                              onChange('cameraMacAddress', (event.target as HTMLInputElement).value)}
                          />
                          <small class="site-form-text">
                            {show('camera_mac_address_help')}{' '}
                            <a
                              id="generate-mac-address"
                              href="#"
                              onClick={(event) => { event.preventDefault(); onGenerateMac(); }}
                            >{show('generate_mac')}</a>
                          </small>
                        </div>

                        <div class="mb-4">
                          <label class="site-form-label required" for="camera_camera_ip_address">
                            {t('activemodel.attributes.camera.camera_ip_address')}
                          </label>
                          <input
                            class="site-form-control"
                            id="camera_camera_ip_address"
                            name="camera[camera_ip_address]"
                            type="text"
                            required
                            pattern={doc?.patterns.ip}
                            title={show('camera_ip_address_title')}
                            value={settings.cameraIpAddress}
                            onInput={(event) =>
                              onChange('cameraIpAddress', (event.target as HTMLInputElement).value)}
                          />
                        </div>

                        <div class="mb-4">
                          <label class="site-form-label required" for="camera_server_ip_address">
                            {t('activemodel.attributes.camera.server_ip_address')}
                          </label>
                          <input
                            class="site-form-control"
                            id="camera_server_ip_address"
                            name="camera[server_ip_address]"
                            type="text"
                            required
                            pattern={doc?.patterns.ip}
                            title={show('server_ip_address_title')}
                            value={settings.serverIpAddress}
                            onInput={(event) =>
                              onChange('serverIpAddress', (event.target as HTMLInputElement).value)}
                          />
                        </div>
                      </div>

                      <div class="site-col">
                        <div class="mb-4">
                          {/*
                            aria-required rather than `required`: the field is
                            mandatory and the asterisk is a CSS ::after a screen
                            reader does not read, but a `required` select makes
                            Rails prepend a blank option -- a chip of "", which
                            decides which layouts and editions are offered and
                            whose output is pasted into a bootloader.
                          */}
                          <label class="site-form-label required" for="camera_flash_type">
                            {t('activemodel.attributes.camera.flash_type')}
                          </label>
                          <select
                            class="site-form-select"
                            id="camera_flash_type"
                            name="camera[flash_type]"
                            aria-required="true"
                            value={settings.flashType}
                            onChange={(event) =>
                              onChange('flashType', (event.target as HTMLSelectElement).value)}
                          >
                            {(narrowed?.chips
                              ?? FLASH_CHIPS.map((value) => ({ value, disabled: false })))
                              .map(({ value, disabled }) => (
                                <option key={value} value={value} disabled={disabled}>
                                  {t(`flash_chip.${value}`)}
                                </option>
                              ))}
                          </select>
                          <small class="site-form-text">
                            {t('activerecord.help.camera.flash_type')}
                          </small>
                        </div>

                        {/* Hidden for NAND, which has one layout and nothing to choose. */}
                        <div id="partition-layout-field" hidden={narrowed?.layoutFieldHidden}>
                          <div class="mb-4">
                            <label class="site-form-label" for="camera_partition_layout">
                              {t('activemodel.attributes.camera.partition_layout')}
                            </label>
                            <select
                              class="site-form-select"
                              id="camera_partition_layout"
                              name="camera[partition_layout]"
                              value={settings.partitionLayout ?? ''}
                              onChange={(event) =>
                                onChange('partitionLayout', (event.target as HTMLSelectElement).value)}
                            >
                              {(narrowed?.layouts
                                ?? PARTITION_LAYOUTS.map((value) => ({ value, disabled: false })))
                                .map(({ value, disabled }) => (
                                  <option key={value} value={value} disabled={disabled}>
                                    {t(`flash_layout.${value}`)}
                                  </option>
                                ))}
                            </select>
                            <small class="site-form-text">
                              {t('activerecord.help.camera.partition_layout')}
                            </small>
                          </div>
                        </div>

                        <div class="mb-4">
                          <label class="site-form-label" for="camera_firmware_version">
                            {t('activemodel.attributes.camera.firmware_version')}
                          </label>
                          <select
                            class="site-form-select"
                            id="camera_firmware_version"
                            name="camera[firmware_version]"
                            value={settings.firmwareVersion}
                            onChange={(event) =>
                              onChange('firmwareVersion', (event.target as HTMLSelectElement).value)}
                          >
                            {(narrowed?.editions ?? []).map(({ value, disabled }) => (
                              <option key={value} value={value} disabled={disabled}>
                                {versionName(t, value)}
                              </option>
                            ))}
                          </select>
                        </div>
                      </div>
                    </div>

                    <p>
                      <button class="site-btn site-btn-primary" type="submit" disabled={doc === null}>
                        {show('generate_button')}
                      </button>
                    </p>
                  </form>
                </div>
              </>
            )
            : <Unavailable t={t} facts={facts} doc={doc!} />}
        </div>
      </article>
    </div>
  );
}

/**
 * What a SoC with no instructions to give says instead.
 *
 * One message per state, not one for everything: for the 25 `neq` chips the
 * truth is that we have the SDK and no board, for the `hlp` ones that we have
 * the board and need a developer, and each of those is something a reader can
 * act on. Firmware published but no bootloader is a different thing again, and
 * "working hard to release firmware" beside a link to that very firmware helps
 * nobody.
 */
function Unavailable({ t, facts, doc }: { t: Props['t']; facts: SocFacts; doc: WizardDocument }) {
  const show = (key: string, options?: Record<string, unknown>) =>
    t(`cameras.socs.show.${key}`, options);

  return (
    <>
      <div class="site-col-md-6 site-col-xl-7 site-col-xxl-8 mb-4">
        <div class="site-alert site-alert-info">
          {doc.availability === 'firmware_only'
            ? (
              <>
                <p>{show('no_bootloader_alert', { name: facts.fullName })}</p>
                <p class="mb-0" dangerouslySetInnerHTML={{ __html: show('stock_bootloader_html') }} />
              </>
            )
            : (
              <p
                class="mb-0"
                dangerouslySetInnerHTML={{
                  __html: t(`cameras.socs.show.unavailable_${facts.status}_html`, {
                    name: facts.fullName,
                    fallback: show('unavailable_html', { name: facts.fullName }),
                  }),
                }}
              />
            )}
        </div>
      </div>

      <div class="site-col">
        {/*
          A link we cannot honour is worse than no link: it reads as our
          download being broken. Both of these are asked of the release index,
          not built from a filename.
        */}
        {doc.bootloader_published && (
          <div class="site-alert site-alert-info github">
            <Icon name="github" size="github" class="float-end" />
            <h5 class="site-h5 mb-1"><a href={doc.bl_url}>{show('bootloader')}</a></h5>
            <p class="mb-0 text-[.875em]">{show('for', { name: facts.fullName })}</p>
          </div>
        )}
        {doc.published.map((bundle) => (
          <div key={`${bundle.flash_type}-${bundle.release}`} class="site-alert site-alert-primary github">
            <Icon name="github" size="github" class="float-end" />
            <h5 class="site-h5 mb-1">
              <a href={bundle.url} title={bundle.filename}>
                {show('bundle', {
                  name: `${versionName(t, bundle.release)} ${bundle.flash_type.toUpperCase()}`,
                })}
              </a>
            </h5>
            <p class="mb-0 text-[.875em]">{show('for', { name: facts.fullName })}</p>
          </div>
        ))}
      </div>
    </>
  );
}
