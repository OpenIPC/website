import { disclaimerConstants } from './constants/constants';

export default function Disclaimer() {
  return(
    <p className="font-sans text-sm text-grey">{disclaimerConstants.text}</p>
  );
}
