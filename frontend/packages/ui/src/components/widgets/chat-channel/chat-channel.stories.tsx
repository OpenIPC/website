import type { Meta, StoryObj } from '@storybook/preact-vite';
import ChatChannel from './Chat-channel';

const meta: Meta<typeof ChatChannel> = {
  component: ChatChannel,
  title: 'Design System/Widgets/Chat Channel',
};

export default meta;

type Story = StoryObj<typeof ChatChannel>;

export const ChatChannelStory: Story = {
  args: {
    header: 'OpenIPC Users(EN)',
    link: 'https://t.me/OpenIPC',
    text: 'International channel about OpenIPC',
  },
};
