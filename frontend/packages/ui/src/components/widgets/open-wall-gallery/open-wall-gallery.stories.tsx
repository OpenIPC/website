import type { Meta, StoryObj } from '@storybook/preact-vite';
import OpenWallGallery from './open-wall-gallery';

const meta: Meta<typeof OpenWallGallery> = {
  component: OpenWallGallery,
  title: 'Design System/Widgets/Open Wall Gallery',
};

export default meta;

const socs = ['HI3516EV300 + IMX335', 'SSC338Q + IMX415', 'GK7205V300 + IMX307'];

export const OpenWallGalleryStory: StoryObj<typeof OpenWallGallery> = {
  args: {
    cameras: Array.from({ length: 9 }, (_, i) => ({
      id: String(i),
      soc: socs[i % socs.length],
      date: '2026-09-21 06:15:0' + (i % 10) + ' UTC',
      firmware: 'OpenIPC 2.3.05.27-lite, majestic',
      uptime: 3600 * (6 + i),
      socTemp: 41 + i,
      resolution: '1920x1080',
      size: 70000 + i * 1300,
    })),
  },
};
