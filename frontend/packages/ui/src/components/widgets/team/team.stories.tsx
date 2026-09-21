import type { Meta, StoryObj } from '@storybook/preact-vite';
import Team from './team';
import { TEAM } from '../../../__fixtures__/team';

const meta: Meta<typeof Team> = {
  component: Team,
  title: 'Design System/Widgets/Team',
};

export default meta;

export const TeamStory: StoryObj<typeof Team> = {
  args: { members: TEAM },
};
