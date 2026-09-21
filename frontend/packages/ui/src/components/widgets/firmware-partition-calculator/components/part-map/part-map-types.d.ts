export type SliceData = {
 width: number,
 color:  `partition${0|1|2|3|4|5|6|7}`,
};

export type PartMapData = { 
  slices: SliceData[],
  freeSpace: string,
};
