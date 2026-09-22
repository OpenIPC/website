import type { Meta, StoryObj } from '@storybook/preact-vite';
import Wallets from './wallets';

const meta: Meta<typeof Wallets> = {
  component: Wallets,
  title: 'Design System/Widgets/Wallets',
};

export default meta;

// Placeholders. The real addresses, if openipc.org ever publishes any, are
// the site's content -- see the note in wallets.tsx.
export const WalletsStory: StoryObj<typeof Wallets> = {
  args: {
    wallets: [
      { title: 'Bitcoin - BTC', address: 'bc1qexampleexampleexampleexampleexampleex', icon: 'Btc' },
      { title: 'TRON (TRC20) - USDT', address: 'TExampleExampleExampleExampleExample', icon: 'Tron' },
      { title: 'The Open Network - TON', address: 'UQExampleExampleExampleExampleExampleExampleExam', icon: 'Ton' },
    ],
    note: 'Example addresses, for the story only.',
  },
};
