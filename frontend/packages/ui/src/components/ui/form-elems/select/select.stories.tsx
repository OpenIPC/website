import type { Meta, StoryObj } from '@storybook/preact-vite';
import Select from './Select';

const meta = {
  component: Select,
  title: 'Design System/UI/Select',
} satisfies Meta<typeof Select>;

export default meta;
type Story = StoryObj<typeof Select>;

export const SelectStoryDefault = {
  args: {
    elemName: 'memChip',
    label: 'Type and size of flash memory chip',
    state: 'default',
    onInput: (e: Event) => console.log(e.target),
    options: [{
      value: 'NOR 8M',
      disabled: false,
    },
    {
      value: 'NOR 16M',
      disabled: true,
    },
    {
      value: 'NOR 32M',
    },
    {
      value: 'NAND'
    }],
    required: true,
    description: 'If you\'re not sure, select NOR 8M',
  },
} satisfies Story;

export const SelectStoryValid = {
  args: {
    elemName: 'memChip',
    label: 'Type and size of flash memory chip',
    state: 'valid',
    onInput: (e: Event) => console.log(e.target),
    options: [{
      value: 'NOR 8M',
      disabled: false,
    },
    {
      value: 'NOR 16M',
      disabled: true,
    },
    {
      value: 'NOR 32M',
    },
    {
      value: 'NAND'
    }],
    required: true,
    description: 'If you\'re not sure, select NOR 8M',
  },
} satisfies Story;

export const SelectStoryInvalid = {
  args: {
    elemName: 'memChip',
    label: 'Type and size of flash memory chip',
    state: 'error',
    onInput: (e: Event) => console.log(e.target),
    options: [{
      value: 'NOR 8M',
      disabled: false,
    },
    {
      value: 'NOR 16M',
      disabled: true,
    },
    {
      value: 'NOR 32M',
    },
    {
      value: 'NAND'
    }],
    required: true,
    description: 'If you\'re not sure, select NOR 8M',
  },
} satisfies Story;

export const SelectStoryDisabled = {
  args: {
    elemName: 'memChip',
    label: 'Type and size of flash memory chip',
    state: 'disabled',
    onInput: (e: Event) => console.log(e.target),
    options: [{
      value: 'NOR 8M',
      disabled: false,
    },
    {
      value: 'NOR 16M',
      disabled: true,
    },
    {
      value: 'NOR 32M',
    },
    {
      value: 'NAND'
    }],
    required: true,
    description: 'If you\'re not sure, select NOR 8M',
  },
} satisfies Story;
