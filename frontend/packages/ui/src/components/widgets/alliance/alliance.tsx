import AllianceIcons from '../../../assets/icons/alliance';
import { allianceConstants } from './constants';

export default function Alliance() {
  const { OpenIPC, Majestic } = AllianceIcons;

  return (
    <div className="flex flex-col">
      <div className="flex flex-row justify-between">
        <a href={allianceConstants.openIPC} className="w-[48%]">
          <OpenIPC />
        </a>
        <a href={allianceConstants.majestic} className="w-[48%]">
          <Majestic />
        </a>
      </div>
    </div>
  );
}
