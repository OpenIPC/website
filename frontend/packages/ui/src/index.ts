/**
 * @openipc/ui — the public surface.
 *
 * Everything openipc.org's pages are assembled from. Nothing under
 * src/__fixtures__/ is exported: those are Storybook data, and
 * src/__tests__/public-surface.test.ts fails if any of it leaks out here.
 */

// --- primitives ------------------------------------------------------------
export { default as IconButton } from './components/ui/buttons/icon-button';
export { default as MainButton } from './components/ui/buttons/main-button';
export { default as ToggleButton } from './components/ui/buttons/toggle-button';
export { default as CustomSelect } from './components/ui/form-elems/customSelect';
export { default as Input } from './components/ui/form-elems/input';
export { default as Radio } from './components/ui/form-elems/radio';
export { default as Select } from './components/ui/form-elems/select';
export { default as H1 } from './components/ui/headers/h1';
export { default as H2 } from './components/ui/headers/h2';

// --- page furniture --------------------------------------------------------
export { default as Header } from './components/widgets/header';
export { default as HeaderMenu } from './components/widgets/header-menu';
export { default as HeaderBurgerButton } from './components/widgets/header-burger-button';
export { default as Footer } from './components/widgets/footer';
export { default as Copyright } from './components/widgets/copyright';
export { default as Socials } from './components/widgets/socials';
export { default as Paragraph } from './components/widgets/paragraph';
export { default as Alliance } from './components/widgets/alliance';
export { default as Disclaimer } from './components/widgets/disclaimer';
export { default as InformationBanner } from './components/widgets/information-banner';
export { default as ModalImage } from './components/widgets/modal-image';
export { default as ChatChannel } from './components/widgets/chat-channel';

// --- hardware catalogue ----------------------------------------------------
export { default as AbcSelector } from './components/widgets/abc-selector';
export { default as VendorsList } from './components/widgets/vendors-list';
export { default as SoCList } from './components/widgets/soc-list';
export { default as SoCListItem } from './components/widgets/soc-list-item';
export { default as SoCManagedList } from './components/widgets/soc-managed-list';
export { default as FirmwareDevStages } from './components/widgets/firmware-dev-stages';

// --- the wall --------------------------------------------------------------
export { default as CameraSnapshot, formatUptime } from './components/widgets/camera-snapshot';
export { default as OpenWallGallery } from './components/widgets/open-wall-gallery';

// --- people and money ------------------------------------------------------
export { default as Team } from './components/widgets/team';
export { default as TeamMember } from './components/widgets/team-member';
export { default as Supporters } from './components/widgets/supporters';
export { default as DonateBanner } from './components/widgets/donate-banner';
export { default as Wallet } from './components/widgets/wallet';
export { default as Wallets } from './components/widgets/wallets';

// --- tools -----------------------------------------------------------------
export { default as FirmwarePartitionCalculator } from './components/widgets/firmware-partition-calculator';
export { default as HighResTimer } from './components/widgets/high-res-timer';
export { default as QrCodeWidget } from './components/widgets/qr-code-widget';
export { default as WannabeKey } from './components/widgets/wannabe-key';

// --- types -----------------------------------------------------------------
export type { SoCItem, SoCManagedListProps, FilterState }
  from './components/widgets/soc-managed-list/types';
export type { SoCListItemProps } from './components/widgets/soc-list-item/soc-list-item';
export type { CamData, CameraSnapshotProps } from './components/widgets/camera-snapshot';
export type { OpenWallGalleryProps } from './components/widgets/open-wall-gallery';
export type { Supporter, SupportersProps } from './components/widgets/supporters';
// The labels a consumer may override on FirmwarePartitionCalculator. The type
// only: the English defaults are the widget's own business, and a capitalised
// value export would read as a component to anyone scanning this file --
// src/__tests__/public-surface.test.ts asserts that it does not.
export type { FwCalcLabels } from './components/widgets/firmware-partition-calculator/types';
export type { MenuItem, MenuItems } from './components/widgets/header-menu/Header-menu';

// --- helpers the widgets are built on --------------------------------------
export { debounce } from './utils';
export { useMediaQuery } from './utils/hooks/useMediaQuery';
export * from './utils/converters';
export * from './utils/validators';
