import { Meta, StoryObj } from '@storybook/preact-vite';
import Alliance from './alliance';

const meta: Meta<typeof Alliance> = {
  component: Alliance,
  title: 'Design System/Widgets/Alliance',
}

export default meta;

type Story = StoryObj<typeof Alliance>;

export const AllianceStory: Story = {};
