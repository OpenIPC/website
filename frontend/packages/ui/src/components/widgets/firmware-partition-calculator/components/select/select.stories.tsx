import type { Meta, StoryObj } from '@storybook/preact-vite';
import Select from './Select';

const meta = {
  component: Select,
  title: 'Design System/UI/Partition Calc Select',
  argTypes: {
    state: {
      options: ['default', 'valid', 'error', 'disabled'],
      control: 'select',
    },
    errorText: {
      options: ['', 'Only number allowed', 'Must not be empty'],
      control: 'select',
    },
    value: {
      options: ['', 'hi_sfc', 'hinand', 'jz_sfc'],
      control: 'select',
    },
    required: {
      options: [false, true],
      control: 'inline-radio',
    },
  },
} satisfies Meta<typeof Select>;

export default meta;

type Story = StoryObj<typeof Select>;

const names = [
  '', 'hi_sfc', 'hinand', 'jz_sfc', 'nor-flash',
  'NOR_FLASH', 'sfc', 'spi0.0', 'spi_flash', 'xm_sfc',
];

export const PartialSelectStory: Story = {
  args: {
    label: 'MTD Device Name',
    elemName: 'MTD-device-name',
    state: 'default',
    onChange: () => {},
    options: names.map(name => ({ value: name, option: name, display: name })),
    value: 'jz_sfc',
    required: false,
  },
};
