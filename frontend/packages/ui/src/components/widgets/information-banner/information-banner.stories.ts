import { Meta, StoryObj } from '@storybook/preact-vite';
import InformationBanner from './information-banner';

const meta = {
  component: InformationBanner,
  title: 'Design System/Widgets/Information Banner',
} satisfies Meta<typeof InformationBanner>;

export default meta;

type Story = StoryObj<typeof InformationBanner>;

export const InformationBannerInformationStory: Story = {
  args: {
    content: 'Laika panda',
    type: 'information',
  },
};

export const InformationBannerWarningStory: Story = {
  args: {
    content: 'Laika panda',
    type: 'warning',
  },
};
