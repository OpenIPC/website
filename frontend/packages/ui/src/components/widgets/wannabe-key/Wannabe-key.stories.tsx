import { Meta, StoryObj } from '@storybook/preact-vite';
import WannabeKey from './Wannabe-key';

const meta = {
  component: WannabeKey,
  title: 'Design System/Widgets/Wannabe Key',
} satisfies Meta<typeof WannabeKey>;

export default meta;
type Story = StoryObj<typeof WannabeKey>;

export const WannabeKeyStory = {
  args: {
    caption: 'Ctrl',
  },
} satisfies Story;
