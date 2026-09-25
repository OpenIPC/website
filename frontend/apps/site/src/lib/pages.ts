/**
 * What renders at each address (#160).
 *
 * The addresses live in ./page-paths.ts, which imports nothing; this file
 * attaches a component to each of them. The split is not tidiness: a vitest
 * run has no Astro plugin, so a test that only wants to know which URLs exist
 * cannot import a module that pulls in 25 `.astro` files.
 *
 * `pagesWithComponents()` refuses to return a partial answer -- an address
 * with no component, or a component under an address the list does not carry,
 * fails the build with the path in the message. Astro would otherwise report
 * the first as a route that renders `undefined`, which is a stack trace rather
 * than a sentence.
 */
import Hardware from '../components/pages/Hardware.astro';
import Wizard from '../components/pages/Wizard.astro';
import Service from '../components/Service.astro';
import Smoke from '../components/Smoke.astro';
import Wall from '../components/pages/Wall.astro';
import Business from '../components/pages/Business.astro';
import Community from '../components/pages/Community.astro';
import Donate from '../components/pages/Donate.astro';
import EdgeAi from '../components/pages/EdgeAi.astro';
import Ecosystem from '../components/pages/Ecosystem.astro';
import FirmwarePartitionsCalculation from '../components/pages/FirmwarePartitionsCalculation.astro';
import GetStarted from '../components/pages/GetStarted.astro';
import GreenLife from '../components/pages/GreenLife.astro';
import HighResolutionTimer from '../components/pages/HighResolutionTimer.astro';
import LowLatency from '../components/pages/LowLatency.astro';
import MajesticEndpoints from '../components/pages/MajesticEndpoints.astro';
import OurTeam from '../components/pages/OurTeam.astro';
import Privacy from '../components/pages/Privacy.astro';
import QrCodeGenerator from '../components/pages/QrCodeGenerator.astro';
import StagesOfFirmwareDevelopment from '../components/pages/StagesOfFirmwareDevelopment.astro';
import Teleoperation from '../components/pages/Teleoperation.astro';
import Utilities from '../components/pages/Utilities.astro';
import WebInterface from '../components/pages/WebInterface.astro';
import { VENDORS } from './hardware';
import { PAGE_PATHS, type PagePath } from './page-paths';

/** One services whitepaper's arguments. See ../components/Service.astro. */
export interface ServiceSpec {
  /** The pages.<key> block, and the URL slug with underscores. */
  key: string;
  /** Four icons for the "what we bring" cards. */
  icons: [string, string, string, string];
  /** How many platform pills the block defines. */
  pills: number;
  mailSubject: string;
  mailBody: string[];
}

export interface PageSpec extends PagePath {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- Astro components share no type
  component: any;
  props?: Record<string, unknown>;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- as above
type Renderer = { component: any; props?: Record<string, unknown> };

/**
 * The five whitepapers are one component and five sets of arguments. What
 * differs between them is an i18n block, four icons, a count of platform pills
 * and the checklist the mailto opens with.
 */
function service(spec: ServiceSpec): Renderer {
  return { component: Service, props: spec as unknown as Record<string, unknown> };
}

const COMPONENTS: Record<string, Renderer> = {
  '/_smoke': { component: Smoke },
  '/_shell/wall': { component: Wall },
  '/open-wall': { component: Wall },

  // The catalogue: one component, three views, sixteen addresses (#162).
  '/supported-hardware/featured': { component: Hardware, props: { view: 'featured' } },
  '/supported-hardware/full-list': { component: Hardware, props: { view: 'full-list' } },
  ...Object.fromEntries(VENDORS.map((vendor) => [
    `/cameras/vendors/${vendor.urlname}`,
    { component: Hardware, props: { view: 'vendor', vendor: vendor.urlname } },
  ])),

  // The wizard, one page per SoC (#164).
  ...Object.fromEntries(VENDORS.flatMap((vendor) => vendor.socs.map((soc) => [
    `/cameras/vendors/${vendor.urlname}/socs/${soc.urlname}`,
    { component: Wizard, props: { vendor: vendor.urlname, soc: soc.urlname } },
  ]))),

  '/business': { component: Business },
  '/community': { component: Community },
  '/donate': { component: Donate },
  '/ecosystem': { component: Ecosystem },
  '/edge-ai': { component: EdgeAi },
  '/get-started': { component: GetStarted },
  '/green_life': { component: GreenLife },
  '/low-latency': { component: LowLatency },
  '/majestic-endpoints': { component: MajesticEndpoints },
  '/our-team': { component: OurTeam },
  '/privacy': { component: Privacy },
  '/stages-of-firmware-development': { component: StagesOfFirmwareDevelopment },
  '/teleoperation': { component: Teleoperation },
  '/utilities': { component: Utilities },
  '/web-interface': { component: WebInterface },

  '/tools/firmware-partitions-calculation': { component: FirmwarePartitionsCalculation },
  '/tools/high-resolution-timer': { component: HighResolutionTimer },
  '/tools/qr-code-generator': { component: QrCodeGenerator },

  '/video-encoding': service({
    key: 'video_encoding',
    icons: ['cash-coin', 'patch-check', 'cpu', 'camera-video'],
    pills: 6,
    mailSubject: 'Encoder audit',
    mailBody: [
      'Encoder platform and codec',
      'Content type and resolution',
      'What you want to cut (bitrate, storage, quality target)',
      'Where the clips can be picked up',
    ],
  }),

  '/isp-sensors': service({
    key: 'isp_sensors',
    icons: ['speedometer2', 'aspect-ratio', 'sliders', 'layers'],
    pills: 5,
    mailSubject: 'Pipeline assessment',
    mailBody: [
      'Sensor and SoC',
      'Mode or picture you need (fps, wide dynamic range, window, latency)',
      'What you have today (driver, firmware, sample unit)',
      'Volume and timeline',
    ],
  }),

  '/reverse-engineering': service({
    key: 'reverse_engineering',
    icons: ['file-earmark-binary', 'shield-check', 'diagram-3', 'lightning-charge'],
    pills: 5,
    mailSubject: 'Target assessment',
    mailBody: [
      'What the target is (blob, firmware, device, model)',
      'What you need from it',
      'What you can share (image, unit, container, sources)',
      'Deadline or dependency',
    ],
  }),

  '/turnkey-hardware': service({
    key: 'turnkey_hardware',
    icons: ['motherboard', 'rocket-takeoff', 'life-preserver', 'clipboard-data'],
    pills: 5,
    mailSubject: 'Product definition sprint',
    mailBody: [
      'What the product does',
      'Sensor, optics and link requirements',
      'Power source, enclosure and cost target',
      'Volume and timeline',
    ],
  }),

  '/digital-twins': service({
    key: 'digital_twins',
    icons: ['hdd-network', 'arrow-repeat', 'broadcast-pin', 'boxes'],
    pills: 4,
    mailSubject: 'Twin feasibility',
    mailBody: [
      'The system (device, controller, radio, robot)',
      'The test you cannot run today',
      'What you can share (firmware image, unit, documentation)',
      'Where the tests should run (your CI or ours)',
    ],
  }),
};

export function pagesWithComponents(): PageSpec[] {
  const orphans = Object.keys(COMPONENTS).filter(
    (path) => !PAGE_PATHS.some((page) => page.path === path),
  );
  if (orphans.length > 0) {
    throw new Error(
      `${orphans.join(', ')} render something but are not in PAGE_PATHS, so nothing routes to them.`,
    );
  }

  return PAGE_PATHS.map((page) => {
    const renderer = COMPONENTS[page.path];
    if (!renderer) throw new Error(`No component for ${page.path}. Add one to COMPONENTS.`);
    return { ...page, ...renderer };
  });
}
