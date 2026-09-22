export type RadioProps<T extends string[]> = {
  name: string,
  captions: T,
  defaultChecked: number,
  changeHandler: (caption: T[number]) => void,
};
