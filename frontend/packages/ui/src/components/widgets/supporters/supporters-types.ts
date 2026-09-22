export type Supporter = {
  /** Shown to screen readers and as the image's alt text. */
  name: string,
  href: string,
  /** Resolved URL. The host app owns these files: on openipc.org the partner
   *  logos live in the Rails tree (app/assets/images/partners/) and #160 turns
   *  the list itself into a data file. The package deliberately ships none. */
  logoUrl: string,
};

export type SupportersProps = {
  supporters: Supporter[],
};
