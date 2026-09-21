import type { Meta, StoryObj } from '@storybook/preact-vite';
import CameraSnapshot from './camera-snapshot';

const meta: Meta<typeof CameraSnapshot> = {
  component: CameraSnapshot,
  title: 'Design System/Widgets/Camera Snapshot',
};

export default meta;

type Story = StoryObj<typeof CameraSnapshot>;

const camera = {
  soc: 'HI3518CV100 + MT9P006',
  date: '2024-05-30 16:45:05 UTC',
  firmware: 'OpenIPC 2.3.05.27-lite, majestic',
  uptime: 191_400,
  socTemp: 45.78,
  resolution: '1920x1080',
  size: 84624,
};

export const CameraSnapshotStory: Story = { args: camera };

export const Loading: Story = { args: { ...camera, loading: true } };

export const WithoutTemperature: Story = {
  args: { ...camera, socTemp: undefined },
};
