import { Meta, StoryObj } from '@storybook/preact-vite';
import QRCodeWidget from './QR-code-widget';

const meta: Meta<typeof QRCodeWidget> = {
  component: QRCodeWidget,
  title: 'Design System/Widgets/QRCodeWidget',
}

export default meta;

export const QRCodeWidgetStory: StoryObj = {
  args: {
    label: 'QR code for network "OpenIPC_NFS"',
    textToCode: 'WIFI:S:OpenIPC_NFS;T:WPA;P:project2021;;',
  },
};
