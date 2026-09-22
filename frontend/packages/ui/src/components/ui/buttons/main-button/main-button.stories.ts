import type { Meta, StoryObj } from '@storybook/preact-vite';
import Button from './Main-button';
import icons from '../../../../assets/icons/ui';

const { Play, Pause, Refresh } = icons;

const meta = {
  component: Button,
  title: 'Design System/UI/Buttons/Main Button',
  argTypes: {
    size: {
      options: ['xs' , 's' , 'm' , 'l'],
      control: { type: 'inline-radio' },
    },
    Icon: {
      options: ['Play', 'Pause', 'Refresh', 'No icon'],
      mapping: {
        'Play': Play,
        'Pause': Pause,
        'Refresh': Refresh,
        'No icon': undefined, 
      },
      control: { type: 'inline-radio' },
    },
    type: {
      options: ['button', 'submit', 'reset'],
      control: { type: 'inline-radio' },
    },
  },
} satisfies Meta<typeof Button>;

export default meta;

type Story = StoryObj<typeof Button>;

export const MainButton: Story = {
  args: {
    type: 'button',
    caption: 'Generate installation guide',
    disabled: false,
    Icon: Refresh,
  },
}
