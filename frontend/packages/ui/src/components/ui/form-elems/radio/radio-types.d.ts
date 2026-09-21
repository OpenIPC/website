export type RadioProps<T extends string[]> = {
  name: string,
  captions: T,
  defaultChecked: number,
  changeHandler: (caption: typeof T[number]) => void,
};
