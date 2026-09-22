import { Meta, StoryObj } from '@storybook/preact-vite';
import Wallet from './wallet';

const meta = {
  component: Wallet,
  title: 'Design System/Widgets/Wallet',
} satisfies Meta<typeof Wallet>;

export default meta;

type Story = StoryObj<typeof Wallet>;

export const WalletStory = {
  args: {
    title: 'Bitcoin - BTC',
    address: '1Gu9xVdXKs5T9TTe2zJn4oSiJs4Dz9Vi6J',
    icon: 'Btc',
  },
} satisfies Story;
