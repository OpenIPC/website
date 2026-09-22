import type { Meta, StoryObj } from '@storybook/preact-vite';
import Input from './Input';
import icons from '../../../../assets/icons/ui';
import { generateRandomMacAddress } from '../../../../utils';

const { Atom } = icons;

const meta = {
  component: Input,
  title: 'Design System/UI/Input',
} satisfies Meta<typeof Input>;

export default meta;

type Story = StoryObj<typeof Input>;

export const InputStoryDefault: Story = {
  args: {
    elemName: 'mac-address',
    value: '',
    type: 'text',
    label: 'Camera MAC address',
    required: true,
    placeholder: 'ff:ff:ff:ff:ff:ff',
    Icon: Atom,
    iconPlace: 'right',
    iconClickHandler: () => console.log(generateRandomMacAddress()),
    state: 'default',
    iconTooltip: 'Generate a random MAC address',
    description: 'MAC address for a device',
  },
}

export const InputStoryValid: Story = {
  args: {
    elemName: 'mac-address',
    type: 'text',
    label: 'Camera MAC address',
    required: true,
    placeholder: 'ff:ff:ff:ff:ff:ff',
    Icon: Atom,
    iconPlace: 'left',
    state: 'valid',
    iconTooltip: 'Generate a random MAC address',
    description: 'MAC address for a device',
  },
}

export const InputStoryError: Story = {
  args: {
    elemName: 'mac-address',
    type: 'text',
    label: 'Camera MAC address',
    required: true,
    placeholder: 'ff:ff:ff:ff:ff:ff',
    Icon: Atom,
    state: 'error',
    errorText: 'Not a valid MAC address',
    iconTooltip: 'Generate a random MAC address',
    description: 'MAC address for a device',
  },
}

export const InputStoryDisabled: Story = {
  args: {
    elemName: 'mac-address',
    type: 'text',
    label: 'Camera MAC address',
    required: true,
    placeholder: 'ff:ff:ff:ff:ff:ff',
    Icon: Atom,
    state: 'disabled',
    iconTooltip: 'Generate a random MAC address',
    description: 'MAC address for a device',
  },
}
