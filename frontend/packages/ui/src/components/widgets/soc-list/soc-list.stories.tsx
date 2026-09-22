import { Meta, StoryObj } from '@storybook/preact-vite';
import SoCList from './soc-list';

const meta: Meta<typeof SoCList> = {
  component: SoCList,
  title: 'Design System/Widgets/SoC List',
}

export default meta;

export const SoCListStory: StoryObj = {
  args: {
    list: [
      {
        model: 'HiSilicon HI3518EV300',
        address: '0x42000000',
        stage: 'DONE',
        installation: 'https://openipc.org/cameras/vendors/hisilicon/socs/hi3518ev300',
      },
      {
        model: 'Goke GK7201V300',
        address: null,
        stage: 'NEQ',
        installation: null,
      },
      {
        model: 'HiSilicon HI3518EV300',
        address: '0x42000000',
        stage: 'DONE',
        installation: 'https://openipc.org/cameras/vendors/hisilicon/socs/hi3518ev300',
      },
      {
        model: 'Goke GK7201V300',
        address: null,
        stage: 'NEQ',
        installation: null,
      }
    ],
  },
}
