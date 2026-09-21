import type { Meta, StoryObj } from '@storybook/preact-vite';
import Radio from './Radio';

const meta = {
  component: Radio,
  title: 'Design System/UI/Radio',
} satisfies Meta<typeof Radio>;

export default meta;

type Story = StoryObj<typeof Radio>;

export const RadioStory: Story = {
  args: {
    name: 'qr-code-generator',
    defaultChecked: 1,
    captions: ['vCard', 'MeCard', 'OpenIPC'],
    changeHandler: (caption: string) => console.log(caption),
  },
}
