export const generateRandomMacAddress = ():string => {
  let ret: string[] = [];
  for (let i = 0; i < 6; i++) {
    let res = Math.floor(Math.random() * 0xff);
    if (i === 0) res = res & (0xff - 0x1) | 0x2;
    ret = [...ret, res > 0xf ? res.toString(16) : `0${res.toString(16)}`];
  }
  return ret.join(':').toUpperCase();
}
