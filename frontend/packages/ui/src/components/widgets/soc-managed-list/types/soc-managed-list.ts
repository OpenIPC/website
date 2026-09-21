export type SoCItem = {
  vendor: string,
  group: string,
  model: string,
  address: null | string,
  stage: 'NEQ' | 'RND' | 'HLP' | 'WIP' | 'MVP' | 'DONE',
  bootloader: string,
  firmware: string,
  featured: boolean,
  core: null | string,
  ai: null | number,
  package: null | string,
  encoder: null | string,
  memory: null | number,
}

export type SoCManagedListProps = {
  fullList: SoCItem[],
}

export type FilterState = {
  abcSelector: null | string,
  vendorSelector: null | string
}
