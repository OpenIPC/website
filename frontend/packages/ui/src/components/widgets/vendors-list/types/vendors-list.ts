export type vendorsListProps = {
  list: string[],
  curSelected: string | null,
  clickHandler: (vendor: string) => void,
};
