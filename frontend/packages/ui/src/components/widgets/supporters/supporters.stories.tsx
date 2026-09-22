import type { Meta, StoryObj } from '@storybook/preact-vite';
import Supporters from './supporters';
import { SUPPORTERS } from '../../../__fixtures__/supporters';

const meta: Meta<typeof Supporters> = {
  component: Supporters,
  title: 'Design System/Widgets/Supporters',
};

export default meta;

export const SupportersStory: StoryObj<typeof Supporters> = {
  args: { supporters: SUPPORTERS },
};
