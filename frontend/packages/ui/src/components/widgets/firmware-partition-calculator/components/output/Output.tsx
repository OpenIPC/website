import { OutputProps } from './output-types';

export default function Output({ label, data }: OutputProps) {
  return (
    <div className="flex min-h-[76px] flex-col rounded-md border bg-wallet-bg">
      <p className="mt-0.5 ml-1 truncate text-sm text-dark-grey">{label}</p>
      <p className="m-2 h-7 py-1 pl-1 font-mono text-xl text-brand-blue" dir="rtl">{data}</p>
    </div>
  );
}
