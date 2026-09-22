import type { Meta, StoryObj } from '@storybook/preact-vite';
import HighResTimer from './High-res-timer';

const meta = {
  component: HighResTimer,
  title: 'Design System/Widgets/High Resolution Timer',
} satisfies Meta<typeof HighResTimer>;

export default meta

type Story = StoryObj<typeof HighResTimer>;

export const HighResTimerStory: Story = {};
