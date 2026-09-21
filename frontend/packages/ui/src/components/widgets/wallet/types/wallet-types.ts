/** The marks src/assets/icons/currency/ carries. */
export type CurrencyIconName = 'Btc' | 'Tron' | 'Ton';

export type WalletProps = {
  title: string,
  address: string,
  icon: CurrencyIconName,
}
