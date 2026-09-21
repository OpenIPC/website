import { Meta, StoryObj } from '@storybook/preact-vite';
import QRCodeWidget from './QR-code-widget';

const meta: Meta<typeof QRCodeWidget> = {
  component: QRCodeWidget,
  title: 'Design System/Widgets/QRCodeWidget',
}

export default meta;

export const QRCodeWidgetStory: StoryObj = {
  args: {
    textToCode: 'WIFI:S:OpenIPC_NFS;T:WPA2;P:project2021;;',
  },
};
