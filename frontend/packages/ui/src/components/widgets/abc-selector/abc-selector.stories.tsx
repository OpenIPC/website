import { Meta, StoryObj } from '@storybook/preact-vite';
import AbcSelector from './abc-selector';

const meta: Meta<typeof AbcSelector> = {
  component: AbcSelector,
  title: 'Design System/Widgets/Abc Selector',
}

export default meta;

type Story = StoryObj<typeof AbcSelector>;

export const AbcSelectorStory: Story = {
  args: {
    letters: ['A', 'B', 'C', 'E', 'F', 'G', 'H', 'I', 'G', 'K'],
  }
}

