import icons from '../../../assets/icons/social';

export default function ChatChannel({ header, link, text }: {header: string, link: string, text: string}) {

  const { Telegram } = icons;

  return (
    <li className="
      w-full list-none rounded-sm border border-light-blue bg-donban-bg p-2
    ">
      <div className="flex flex-row gap-x-4">
        <div className="flex grow flex-col">
          <dl>
            <dt className="text-base font-bold text-text-blue underline">
              <a href={link}>{header}</a>
            </dt>
            <dd className="mt-2 text-base text-action-blue">
              {text}
            </dd>
          </dl>
        </div>
        <div className="*:size-[40px]">
          <Telegram />
        </div>
      </div>
    </li>
  );
}
