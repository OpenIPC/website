import { Meta, StoryObj } from '@storybook/preact-vite';
import FirmwareDevStages from './firmware-dev-stages';

const meta: Meta<typeof FirmwareDevStages> = {
  component: FirmwareDevStages,
  title: 'Design System/Widgets/Firmware Dev Stages',
}

export default meta;

type Story = StoryObj<typeof FirmwareDevStages>;

export const FirmwareDevStagesStory: Story = {};
