import { TeamMemberProps } from './types';
import bg from '../../../assets/icons/ui/camera-preview-background.svg';
import UIIcons from '../../../assets/icons/social';

export default function TeamMember(props: TeamMemberProps) {
  const { imgSrc, name, bio, socials } = props;

  return (
    <li className="w-full list-none rounded-md border border-wallet-border">
      <div className="aspect-square rounded-t-md bg-repeat" style={`background-image: url(${bg})`}>
        { imgSrc && <img src={imgSrc} className="w-full rounded-t-md"></img> }
      </div>
      <div className="relative min-h-[160px] p-3">
        <h5 className="pb-2 text-xl font-normal">{name}</h5>
        <p className="text-sm">{bio}</p>
        {socials && socials.length &&
          <ul className="
            absolute -top-3.5 right-2 flex max-w-fit flex-row gap-x-2
          ">
            {socials.map(social => {
              const Icon = UIIcons[social.icon];
              return (<li key={social.link} className="
                flex size-6 flex-col items-center justify-center rounded-full
                border border-white bg-white
              ">
                <a href={social.link} className="*:w-[20px]"><Icon /></a>
              </li>)
            })}
          </ul>
        }
      </div>
    </li>
  );
}
