import type { WalletProps } from '../wallet/types/wallet-types';

export type WalletsProps = {
  wallets: WalletProps[],
  /** Rendered under the list. Left out, the note is not shown. */
  note?: import('preact').ComponentChildren,
};
