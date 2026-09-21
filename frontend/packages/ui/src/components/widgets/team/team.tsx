import type { TeamProps } from './team-types';
import TeamMember from '../team-member/team-member';

export default function Team(props: TeamProps) {
  const { members } = props;

  return (
    <ul className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-5">
      {members.map(member => {
        const { imgSrc, name, bio, socials } = member;
        return <TeamMember key={name} imgSrc={imgSrc} name={name} bio={bio} socials={socials} />
      })}
    </ul>
  );
}
