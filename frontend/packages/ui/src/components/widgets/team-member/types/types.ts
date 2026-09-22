/** The marks src/assets/icons/social/ carries. */
export type SocialIconName =
  'Github' | 'Telegram' | 'Twitter' | 'Facebook' | 'YouTube' | 'OpenCollective';

export type TeamMemberProps = {
  imgSrc?: string,
  name: string,
  bio: string,
  socials?: {
      link: string,
      icon: SocialIconName,
    }[],
}
