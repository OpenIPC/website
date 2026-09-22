import Wallet from '../wallet/wallet';
import type { WalletsProps } from './wallets-types';

/**
 * The package ships no addresses. fancyweb-ng hard-coded three -- BTC, USDT
 * on TRC20 and TON -- which openipc.org's donate page does not publish at
 * all, and an unverifiable payment address is not something a component
 * library should be the source of.
 */
export default function Wallets({ wallets, note }: WalletsProps) {
  return (
    <div className="flex w-full max-w-96 flex-col gap-y-2">
      {wallets.map(w => <Wallet key={w.address} {...w} />)}
      {note && <p className="text-[13px]">{note}</p>}
    </div>
  );
}
