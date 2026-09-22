export function isNonEmpty(val: string) {
  return !!val;
}

export function isDigitsOnly(val: string) {
  return /^\d+$/.test(val);
}

export function isDecOrHexNumber(val: string) {
  return /^((0[xX][0-9a-fA-F]*)|\d+)$/.test(val);
}

export function isValidHex(val: string) {
  return /^0[xX][0-9a-fA-F]+$/.test(val);
}

export function isValidMAC(val: string) {
  // [0-9A-Faf] upstream: A-F, then a literal 'a' and 'f'. It rejected
  // aa:bb:cc:dd:ee:ff -- which is how Linux prints a MAC.
  return /^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$/.test(val);
}

export function isValidIP(val: string) {
  return /^(?!0\.0\.0\.0|255\.255\.255\.255)((((2([0-4][0-9]|5[0-5]))|1[0-9]{2}|[0-9]{1,2})\.){3}(((2([0-4][0-9]|5[0-5]))|1[0-9]{2}|[0-9]{1,2})))$/.test(val);
}
