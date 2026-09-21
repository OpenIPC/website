import type { MenuItems } from './Header-menu';

const ABOUT: MenuItems = [
  {
    id: 'aee8efec-4c87-4f6f-904d-dda737a0d1ab',
    label: 'Our team',
    type: 'link',
    url: '/our-team',
  },
  {
    id: 'aee8efec-4c87-4f6f-904d-dda737a0d1az',
    label: 'Our projects',
    type: 'link',
    url: '/our-projects',
  },
]

const TOOLS: MenuItems = [
  {
    id: '46360be8-f6d5-456f-80cd-1868ef679e3e',
    label: 'QR code generator',
    type: 'link',
    url: '/tools/qr-code-generator',
  },
  {
    id: 'a4245f92-8bd9-4b13-a928-6e54b8938be4',
    label: 'High-Resolution timer',
    type: 'link',
    url: '/tools/high-resolution-timer',
  },
];

export const MENU_ITEMS: MenuItems = [
  {
    id: 'aee8efec-4c87-4f6f-904d-dda737a0d1ab',
    label: 'Introduction',
    type: 'link',
    url: '/introduction',
  },
  {
    id: 'f7484376-5973-4c61-ad50-38967c6dd12f',
    label: 'Supported hardware',
    type: 'link',
    url: '/supported-hardware',
  },
  {
    id: '4854f043-de17-450b-b170-381707273f48',
    label: 'It\'s open source',
    type: 'link',
    url: '/support-open-source',
  },
  {
    id: '545adc16-f16b-4877-99fb-1712e00db356',
    label: 'About',
    type: 'parent',
    children: ABOUT,
  },
  {
    id: 'e9daf615-b942-40c1-aac9-93369ebd5287',
    label: 'Tools',
    type: 'parent',
    children: TOOLS,
  },
];
