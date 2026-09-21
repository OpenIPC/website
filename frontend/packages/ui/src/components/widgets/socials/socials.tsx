import SocialIcons from '../../../assets/icons/social';
import { socialContants } from './constants';

export default function Socials() {
  return (
    <ul className="flex max-w-max flex-row">
      {socialContants.map((social) => {
        const Icon = SocialIcons[social.title];
        return (
          <li key={social.title} className="
            border border-grey-bg transition-colors duration-500
            first:rounded-l-md
            last:rounded-r-md
            hover:border-black
          ">
            <a href={social.link} className="
              flex min-h-10 min-w-10 flex-col items-center justify-center
              *:size-[16px]
            ">
              <Icon />
            </a>
          </li>
        );
      })}
    </ul>
  );
}
