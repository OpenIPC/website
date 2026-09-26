/**
 * The installation wizard, in the browser (#164).
 *
 * One address per SoC, as it has been since #156: bare it is the form, with
 * `camera[...]` it is the instructions that form produces. The page is
 * prerendered in the first of those states and switches to the second without
 * a round trip, so the address a visitor can share is the address the reference
 * answers to, character for character.
 *
 * What this file does NOT do is compute anything about flash. Every command
 * comes from `WizardExport` (#163) with three holes in it -- the two
 * addresses and the MAC -- and filling those is `wizard-input.ts`'s job, under
 * the patterns the export carries. Nothing about where a partition starts
 * exists in JavaScript.
 */
import { useEffect, useMemo, useState } from 'preact/hooks';
import type { ComponentChildren } from 'preact';
import { combinationFor, type WizardDocument } from '../../lib/wizard-export';
import {
  DEFAULTS, fromPermalink, type WizardSettings,
} from '../../lib/wizard-input';
import { generateMac, narrow, openOn } from '../../lib/wizard-menu';
import {
  fromForm, isFormSubmission, settle, toFormQuery, type FlashMessage,
} from '../../lib/wizard-result';
import { useWizardTranslations } from '../../lib/wizard-i18n';
import type { Locale } from '../../lib/i18n';
import Form, { versionName, type SocFacts } from './Form.tsx';
import Result, { type ResultLinks, type SupportLabels } from './Result.tsx';
import Special from './Special.tsx';

interface Props {
  locale: Locale;
  /** Where to fetch this SoC's commands from. */
  source: string;
  facts: SocFacts & { segment: string };
  links: ResultLinks;
  /** The wizard's own address, for the permanent link and the download. */
  here: string;
  supportGoal: number;
  supportLabels: SupportLabels;
  warningSrc: string;
}

export default function Wizard({
  locale, source, facts, links, here, supportGoal, supportLabels, warningSrc,
}: Props) {
  const t = useWizardTranslations(locale);

  const [doc, setDoc] = useState<WizardDocument | null>(null);
  const [failed, setFailed] = useState(false);
  const [settings, setSettings] = useState<WizardSettings>({ ...DEFAULTS });
  const [layoutChosen, setLayoutChosen] = useState(false);
  const [query, setQuery] = useState<URLSearchParams | null>(null);

  // The address decides which of the two pages this is, and the Back button
  // has to move between them: a submission is a history entry, not a mode.
  useEffect(() => {
    const read = () => setQuery(new URLSearchParams(window.location.search));
    read();
    window.addEventListener('popstate', read);
    return () => window.removeEventListener('popstate', read);
  }, []);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const response = await fetch(source, { headers: { accept: 'application/json' } });
        if (!response.ok) throw new Error(String(response.status));
        const loaded = (await response.json()) as WizardDocument;
        if (live) setDoc(loaded);
      } catch {
        // Said on the page rather than only logged: the form cannot narrow its
        // menus or produce a command without this, and a form that looks ready
        // and is not is worse than one that says so.
        if (live) setFailed(true);
      }
    })();
    return () => { live = false; };
  }, [source]);

  // Where the form opens. A link is validated exactly as the form is -- it is
  // the one door a stranger can send somebody through.
  useEffect(() => {
    if (!doc || !query || isFormSubmission(query)) return;

    const asked = fromPermalink(query, doc.patterns);
    const opened = openOn(
      { chip: asked.flashType, layout: asked.partitionLayout, edition: asked.firmwareVersion },
      doc.editions,
      doc.offerable,
    );

    setLayoutChosen(opened.layoutChosen);
    setSettings({
      ...asked,
      flashType: opened.chip,
      partitionLayout: opened.layout === '' ? undefined : opened.layout,
      firmwareVersion: opened.edition,
    });
  }, [doc, query]);

  const narrowed = useMemo(() => {
    if (!doc) return null;
    return narrow(
      {
        chip: settings.flashType,
        layout: settings.partitionLayout ?? '',
        edition: settings.firmwareVersion,
        layoutChosen,
      },
      doc.editions,
      doc.offerable,
    );
  }, [doc, settings, layoutChosen]);

  // Keep the settings on what the menus settled on, so what is submitted is
  // what is on screen.
  useEffect(() => {
    if (!narrowed) return;
    const layout = narrowed.layout === '' ? undefined : narrowed.layout;
    if (narrowed.chip === settings.flashType
      && layout === settings.partitionLayout
      && narrowed.edition === settings.firmwareVersion) return;

    setSettings((current) => ({
      ...current,
      flashType: narrowed.chip,
      partitionLayout: layout,
      firmwareVersion: narrowed.edition,
    }));
  }, [narrowed]);

  const submitted = query !== null && isFormSubmission(query);

  const result = useMemo(() => {
    if (!doc || !query || !submitted) return null;

    const asked = fromForm(query, doc.patterns);
    const settled = settle(asked, {
      patterns: doc.patterns,
      editions: doc.editions,
      defaultFlashChip: doc.default_flash_chip,
      specialPages: doc.special_pages,
    });

    return { ...settled, combination: combinationFor(doc, settled.settings) };
  }, [doc, query, submitted]);

  // `wizard-page` is not decoration: it scopes Bootstrap's reboot margins to
  // this page, whose markup is the ERB's and expects them.
  const page = (children: ComponentChildren) => <div class="wizard-page">{children}</div>;

  if (submitted) {
    // Nothing is rendered from a half-known state: every step, every link and
    // every command on the result page comes out of the export, and guessing
    // at any of them is how the two halves come to disagree about a camera
    // somebody is about to flash.
    if (failed) return page(<Unreachable t={t} facts={facts} source={source} />);
    if (!doc || !result) return null;

    if (result.page) {
      return page(
        <Special
          t={t}
          facts={facts}
          page={result.page}
          edition={versionName(t, result.settings.firmwareVersion)}
          flashTypeName={t(`flash_chip.${result.settings.flashType}`)}
          warningSrc={warningSrc}
        />,
      );
    }

    if (!result.combination) return page(<Unreachable t={t} facts={facts} source={source} />);

    return page(
      <Result
        t={t}
        facts={facts}
        doc={doc}
        combination={result.combination}
        settings={result.settings}
        flashes={result.flashes as FlashMessage[]}
        links={links}
        supportGoal={supportGoal}
        supportLabels={supportLabels}
        locale={locale}
        here={here}
      />,
    );
  }

  return page(
    <Form
      t={t}
      facts={facts}
      doc={doc}
      settings={settings}
      narrowed={narrowed}
      failed={failed}
      onChange={(field, value) => {
        if (field === 'partitionLayout') setLayoutChosen(true);
        setSettings((current) => ({ ...current, [field]: value }));
      }}
      onGenerateMac={() => {
        if (settings.cameraMacAddress !== '') {
          window.alert(t('cameras.socs.show.mac_already_set'));
          return;
        }
        setSettings((current) => ({ ...current, cameraMacAddress: generateMac() }));
      }}
      onSubmit={() => {
        // pushState rather than a navigation: the address is the one the reference
        // answers to either way, and the page it would fetch is the one
        // already open.
        window.history.pushState({}, '', here + toFormQuery(settings));
        setQuery(new URLSearchParams(toFormQuery(settings)));
      }}
    />,
  );
}

/**
 * What the page says when it cannot get the commands.
 *
 * Named, not silent, and not a spinner that never stops: the reader came here
 * for a block to paste into a bootloader, and the honest answer is that this
 * site cannot produce one right now and which link still works.
 */
function Unreachable({ t, facts, source }: {
  t: (key: string, options?: Record<string, unknown>) => string;
  facts: SocFacts; source: string;
}) {
  return (
    <div class="site-container">
      <div class="site-alert site-alert-warning mt-12" role="alert">
        <p class="mb-0">
          {t('cameras.socs.show.export_unreachable')}{' '}
          <a href={facts.socHref}>{t('cameras.socs.show.export_unreachable_link')}</a>
        </p>
      </div>
      <p class="mt-4 text-[.875em] text-body-secondary">
        <code>{source}</code>
      </p>
    </div>
  );
}
