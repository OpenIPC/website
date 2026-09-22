import { Meta, StoryObj } from '@storybook/preact-vite';
import TeamMember from './team-member';

const meta: Meta<typeof TeamMember> = {
  component: TeamMember,
  title: 'Design System/Widgets/Team Member',
}

export default meta;

type Story = StoryObj<typeof TeamMember>;

export const TeamMemberStory: Story = {
  args: {
    imgSrc: 'https://avatars.githubusercontent.com/u/31137747',
    name: 'yarobash',
    bio: 'A narrow-known programmer and an ardent follower of free software, he does not tolerate being tied to vendors, but is ready to do something for money.',
    socials: [
      {
        link: 'est',
        icon: 'Github',
      },
      {
        link: 'toje est',
        icon: 'Telegram',
      }
    ],
  },
}

export const SecretTeamMember: Story = {
  args: {
    name: 'Clandestino',
    bio: 'A este programador le gusta estar en las sombras, no necesita gran fama, simplemente hace su trabajo.',
  },
}
