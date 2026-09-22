export type SliceData = {
 width: number,
 /** 'reserved' is the initial offset: occupied, but not a partition. */
 color: `partition${0|1|2|3|4|5|6|7}` | 'reserved',
};

export type PartMapData = { 
  slices: SliceData[],
  freeSpace: string,
};
