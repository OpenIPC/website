import type { SliceData, PartMapData } from './part-map-types';

export default function PartitionMap({ slices, freeSpace }: PartMapData) {
  
  function getStyle(slice: SliceData) {
    const { color } = slice;
    const bgs = {
      'partition0': 'bg-partition0',
      'partition1': 'bg-partition1',
      'partition2': 'bg-partition2',
      'partition3': 'bg-partition3',
      'partition4': 'bg-partition4', 
      'partition5': 'bg-partition5', 
      'partition6': 'bg-partition6',
      'partition7': 'bg-partition7',
      // Occupied by the initial offset. Not a partition, and not free --
      // the map used to leave it in the grey remainder, where it reads as
      // room for another partition.
      'reserved': 'bg-dark-grey',
    };

    return `${bgs[color]}`;
  }

  return (
    <div className="
      relative flex h-8 w-full flex-row overflow-hidden rounded-md border-0
      bg-light-grey
    ">
      {
        // eslint-disable-next-line @eslint-react/no-array-index-key -- a partition map is positional; slices have no identity but their place
      !!slices.length && slices.map((slice, i) => (<div key={i} className={`
        ${getStyle(slice)}
      `} style={`width:${slice.width}%;`}></div>))
      }
      <div className="
        absolute inset-0 flex flex-row items-center justify-center
      ">
        <span className="text-lg text-dark-grey">Free space: {freeSpace}</span>
      </div>
    </div>
  );
}
