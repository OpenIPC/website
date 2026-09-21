import { Meta, StoryObj } from '@storybook/preact-vite';
import Copyright from './copyright';

const meta: Meta<typeof Copyright> = {
  component: Copyright,
  title: 'Design System/Widgets/Copyright',
}

export default meta;

type Story = StoryObj<typeof Copyright>;

export const CopyrightStory: Story = {};
