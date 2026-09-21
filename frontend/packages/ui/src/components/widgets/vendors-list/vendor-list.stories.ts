import { Meta, StoryObj } from '@storybook/preact-vite';
import VendorsList from './vendors-list';
import { vendorsListConstants } from './constants/vendor-list';

const meta: Meta<typeof VendorsList> = {
  component: VendorsList,
  title: 'Design System/Widgets/Vendors List',
}

export default meta;

type Story = StoryObj<typeof VendorsList>;

export const VendorsListStory: Story = {
  args: {
    list: vendorsListConstants,
  }
}
