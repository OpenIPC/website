import type { Meta, StoryObj } from '@storybook/preact-vite';
import SoCManagedList from './soc-managed-list';
import { SOCS } from '../../../__fixtures__/socs';

const meta: Meta<typeof SoCManagedList> = {
  component: SoCManagedList,
  title: 'Design System/Widgets/SoC Managed List',
};

export default meta;

export const SoCManagedListStory: StoryObj<typeof SoCManagedList> = {
  args: { fullList: SOCS },
};
