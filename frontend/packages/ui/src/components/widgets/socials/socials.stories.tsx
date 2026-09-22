import { Meta, StoryObj } from '@storybook/preact-vite';
import Socials from './socials';

const meta: Meta<typeof Socials> = {
  component: Socials,
  title: 'Design System/Widgets/Socials',
}

export default meta;

type Story = StoryObj<typeof Socials>;

export const SocialsStory: Story = {};
