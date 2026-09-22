import { Meta, StoryObj } from '@storybook/preact-vite';
import Disclaimer from './disclaimer';

const meta: Meta<typeof Disclaimer> = {
  component: Disclaimer,
  title: 'Design System/Widgets/Disclaimer',
}

export default meta;

type Story = StoryObj<typeof Disclaimer>;

export const DisclaimerStory: Story = {};
