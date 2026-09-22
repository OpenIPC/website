export default function WannabeKey({ caption }: { caption: string }) {
  return (
    <span className="
      rounded-sm border border-grey bg-linear-to-t from-wallet-border to-grey-bg
      px-2 py-0.5 text-sm font-bold text-dark-grey
    ">{caption}</span>
  );
}
