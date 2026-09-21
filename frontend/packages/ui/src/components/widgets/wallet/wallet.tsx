import { WalletProps } from './types/wallet-types';
import currencyIcons from '../../../assets/icons/currency';

export default function Wallet({title, address, icon}: WalletProps) {
  const Icon = currencyIcons[icon];
  return (
    <div className="
      flex flex-col gap-y-3 rounded-sm border border-wallet-border bg-wallet-bg
      p-4
    ">
      <div className="flex flex-row">
        <p className="shrink grow text-lg font-bold">{title}</p>
        <div className="*:size-6">
          <Icon />
        </div>
      </div>
      <p className="w-full text-base break-all text-black opacity-75">{address}</p>
    </div>
  );
}
