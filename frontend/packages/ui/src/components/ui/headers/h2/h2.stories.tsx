import type { Meta, StoryObj } from '@storybook/preact-vite';
import H2 from './h2';

const meta = {
  component: H2,
  title: 'Design System/UI/Headers/H2',
} satisfies Meta<typeof H2>;

export default meta;
type Story = StoryObj<typeof meta>;

export const H2Story: Story = {
  args: {
    content: 'OpenIPC is an alternative open firmware for your IP camera.',
  },
};
