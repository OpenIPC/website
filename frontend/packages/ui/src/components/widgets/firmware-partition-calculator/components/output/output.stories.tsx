import type { Meta, StoryObj } from '@storybook/preact-vite';
import Output from './Output';

const meta = {
  component: Output,
  title: 'Design System/UI/Partition Calc Output',
} satisfies Meta<typeof Output>; 

export default meta;

type Story = StoryObj<typeof Output>;

export const PartCalOutputStory: Story = {
  args: {
    label: 'MTD device name',
    data: '0x0543AF',
  },
}
