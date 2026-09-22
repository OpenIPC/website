import type { Meta, StoryObj } from '@storybook/preact-vite';
import H1 from './h1';

const meta = {
  component: H1,
  title: 'Design System/UI/Headers/H1',
} satisfies Meta<typeof H1>;

export default meta;
type Story = StoryObj<typeof meta>;

export const H1Story: Story = {
  args: {
    content: 'Introduction',
  },
};
