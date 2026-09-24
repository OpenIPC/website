/**
 * The two combinations that get a page of their own.
 *
 * `Cameras::SocsController#update` answers SigmaStar on NAND and the two
 * HI3536 NVRs this way: the procedure is different enough that showing the
 * generated commands would be wrong rather than incomplete. Which one applies
 * is the export's answer, not a rule re-derived here.
 */
import { Breadcrumb, type SocFacts } from './Form.tsx';

type Translate = (key: string, options?: Record<string, unknown>) => string;

const WIKI: Record<string, string> = {
  sigmastar_nand: 'https://github.com/OpenIPC/wiki/blob/master/en/fpv-sigmastar.md',
  hi3536dv100: 'https://github.com/OpenIPC/wiki/blob/master/en/fpv-nvr.md',
};

const TEMPLATE: Record<string, string> = {
  sigmastar_nand: 'sigmastar_nand_is_weird',
  hi3536dv100: 'hi3536dv100_is_weird',
};

interface Props {
  t: Translate;
  facts: SocFacts;
  page: string;
  edition: string;
  flashTypeName: string;
  warningSrc: string;
}

export default function Special({ t, facts, page, edition, flashTypeName, warningSrc }: Props) {
  const own = (key: string, options?: Record<string, unknown>) =>
    t(`cameras.socs.${TEMPLATE[page]}.${key}`, options);

  // Only the NVR page names the device in its own words; the SigmaStar one
  // uses the wizard's ordinary subtitle.
  const subtitle = page === 'hi3536dv100'
    ? own('subtitle', { soc_name: facts.fullName, flash_type: flashTypeName })
    : t('firmware.installation.subtitle', { soc_name: facts.fullName, flash_type: flashTypeName });

  return (
    <div class="wizard-body site-container mb-6">
      <Breadcrumb t={t} facts={facts} />

      <article class="firmware">
        <header>
          <a class="float-end" href={facts.stagesHref} title={facts.statusTitle}>
            <img width="64" src={facts.stageSrc} alt="" />
          </a>
          <h2 class="mt-4 mb-0">{t('firmware.installation.title')} ({edition})</h2>
          <p class="site-lead font-bold">{subtitle}</p>
        </header>

        <div class="site-row">
          <div class="site-col site-col-12 site-col-lg-4 site-col-xl-3">
            <img src={warningSrc} alt="" />
          </div>
          <div class="site-col site-col-lg-7 site-col-xl-6 site-col-xxl-5">
            <h2 class="mt-12 mb-4">{t('firmware.installation.attention')}</h2>
            <p class="site-lead">{own('paragraph1')}</p>
            <p>{own('paragraph2')} <a href={WIKI[page]} target="_blank" rel="noreferrer">{WIKI[page]}</a></p>
          </div>
        </div>
      </article>
    </div>
  );
}
