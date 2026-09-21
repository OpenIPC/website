import type { Meta, StoryObj } from '@storybook/preact-vite';
import Input from './Input';

const meta = {
  component: Input,
  title: 'Design System/UI/Partition Calc Input',
  argTypes: {
    label: {
      control: {
        type: 'select',
      },
      options: ['MTD device name', 'Partition 1 name'],
    },
    borderWidth: {
      control: {
        type: 'select',
      },
      options: ['1px', '4px'],
    },
    borderColor: {
      control: {
        type: 'select',
      },
      options: [
        'default',
        'partition1',
        'partition2',
        'partition3',
        'partition4',
        'partition5',
        'partition6',
        'partition7',
        'partition8',
      ],
    },
    dir: {
      options: ['ltr', 'rtl'],
      control: {
        type: 'radio',
      },
    },
    state: {
      options: ['default', 'valid', 'error', 'disabled'],
      control: 'select',
    },
    errorText: {
      options: ['', 'Only number allowed', 'Must not be empty'],
      control: 'select',
    },
  },
} satisfies Meta<typeof Input>;

export default meta;

type Story = StoryObj<typeof Input>;

export const PartCalcInputStory: Story = {
  args: {
    elemName: 'mtd-dev-name',
    label: 'MTD device name',
    borderWidth: '4px',
    borderColor: "partition2",
    value: 'Partition name',
    dir: 'ltr',
    required: true,
    state: 'default',
  },
}
