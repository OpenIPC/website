import SocialIcons from '../../../../assets/icons/social';

type Social<T> = {
  title: T,
  link: string,
}[];

export const socialContants: Social<keyof typeof SocialIcons> = [
  {
    title: 'Github',
    link: 'https://github.com/openipc',
  },
  {
    title: 'Twitter',
    link: 'https://twitter.com/openipc',
  },
  {
    title: 'YouTube',
    link: 'https://www.youtube.com/@openipc',
  },
  {
    title: 'OpenCollective',
    link: 'https://opencollective.com/openipc',
  },
];
