import { Meta, StoryObj } from '@storybook/preact-vite';
import SoCListItem from './soc-list-item';

const meta: Meta<typeof SoCListItem> = {
  component: SoCListItem,
  title: 'Design System/Widgets/SoC List Item',
}

export default meta;

export const SoCListItemFullStory: StoryObj = {
  args: {
    model: 'HiSilicon HI3518EV300',
    address: '0x42000000',
    stage: 'DONE',
    installation: 'https://openipc.org/cameras/vendors/hisilicon/socs/hi3518ev300',
  }
}

export const SoCListItemEmptyStory: StoryObj = {
  args: {
    model: 'Goke GK7201V300',
    address: null,
    stage: 'NEQ',
    installation: null,
  }
}
