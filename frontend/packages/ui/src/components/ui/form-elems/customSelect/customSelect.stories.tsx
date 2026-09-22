import type {Meta, StoryObj} from '@storybook/preact-vite';
import CustomSelect from './CustomSelect';

const meta = {
  component: CustomSelect,
  title: 'Design System/UI/CustomSelect',
  argTypes: {
    open: {
      control: 'boolean',
      description: 'Set opennes of selection box',
    },
    state: {
      control: 'select',
      options: ['default', 'valid', 'error', 'disabled'],
    },
    size: {
      control: 'select',
      options: ['sm', 'md', 'xl'],
    },
  },
} satisfies Meta<typeof CustomSelect>;

export default meta;
type Story = StoryObj<typeof CustomSelect>;

export const CustomSelectStoryDefault = {
  args: {
    elemName: 'chip_prefix',
    state: 'default',
    value: 'xm_sfc',
    onChange: (e: Event) => console.log(e.target),
    options: [
      {value: '',
        option: '',
        display: '',
        disabled: false},
      {value: 'hi_sfc',
        option: 'hi_sfc (HiSilicon)',
        display: 'hi_sfc',
        disabled: false},
      {value: 'hinand',
        option: 'hinand',
        display: 'hinand',
        disabled: false},
      {value: 'jz_sfc',
        option: 'jz_sfc',
        display: 'jz_sfc',
        disabled: false},
      {value: 'nor-flash',
        option: 'nor-flash',
        display: 'nor-flash',
        disabled: false},
      {value: 'NOR_FLASH',
        option: 'NOR_FLASH (SigmaStar)',
        display: 'NOR_FLASH',
        disabled: false},
      {value: 'sfc',
        option: 'sfc',
        display: 'sfc',
        disabled: false},
      {value: 'spi0.0',
        option: 'spi0.0',
        display: 'spi0.0',
        disabled: false},
      {value: 'spi_flash',
        option: 'spi_flash',
        display: 'spi_flash',
        disabled: true},
      {value: 'xm_sfc',
        option: 'xm_sfc',
        display: 'xm_sfc',
        disabled: false},
    ],
    label: 'Network interface',
    description: 'If you\'re not sure, select NOR 8M',
  },
} satisfies Story;
