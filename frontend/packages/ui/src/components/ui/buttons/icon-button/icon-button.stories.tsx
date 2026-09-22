import type {Meta, StoryObj} from '@storybook/preact-vite';
import IconButton from './icon-button';
import UIIcons from '../../../../assets/icons/ui';

const {Burger: BurgerIcon} = UIIcons;

const meta: Meta<typeof IconButton> = {
  component: IconButton,
  title: 'Design System/UI/Buttons/Burger Button',
  decorators: [
    (Story) => <div style="background-color: #4c60d8; width: 100%; height: 88px; display: flex; justify-content: flex-end; align-items: center; padding: 8px;"><Story /></div>,
  ],
};

export default meta;
type Story = StoryObj<typeof IconButton>;

export const BurgerButton: Story = {
  args: {
    withBorder: true,
  },
  render: (args) => <IconButton {...args}>
    <BurgerIcon />
  </IconButton>,
};
