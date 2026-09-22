import Paragraph from '../paragraph/paragraph';
import type {InformationBannerProps} from './information-type';

export default function InformationBanner(
  { content, type }: InformationBannerProps
) {
  function getColorStyle (type: NonNullable<InformationBannerProps['type']>) {
    const stylesFab: Record<NonNullable<InformationBannerProps['type']>, () => void> = {
      information: () => 'text-info-text bg-stages-bg border-stages-border',
      warning: () => 'text-warning-text bg-warning-bg border-warning-border',
    };
    return stylesFab[type]();
  }

  return (
    <div className={`
      flex flex-col gap-y-4 rounded-sm border p-4
      ${type
      ? getColorStyle(type)
      : getColorStyle('information')}
    `}>
      <Paragraph content={content} />
    </div>
  );
}
