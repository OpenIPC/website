import type { partStrProps } from './partition-string-types';

export default function PartitionString({ partStrData }: partStrProps) {
  return (
    <div className="
      flex min-h-14 w-full flex-col items-center justify-center rounded-md
      border border-stages-border bg-stages-bg px-2 py-1
    ">
      <p className="
        text-base break-all text-info-text
        md:text-xl
      ">
        { partStrData }
      </p>
    </div>
  );
}
