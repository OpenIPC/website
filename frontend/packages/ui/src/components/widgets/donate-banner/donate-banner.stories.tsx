import { Meta, StoryObj } from '@storybook/preact-vite';
import DonateBanner from './donate-banner';

const meta: Meta<typeof DonateBanner> = {
  component: DonateBanner,
  title: 'Design System/Widgets/Donate Banner',
}

export default meta;

type Story = StoryObj<typeof DonateBanner>;

export const DonateBannerSmallStory: Story = {
  args: {
    size: 'small',
  },
}

export const DonateBannerBigStory: Story = {
  args: {
    size: 'big',
  },
}
