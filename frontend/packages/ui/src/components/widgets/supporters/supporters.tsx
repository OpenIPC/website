import type { SupportersProps } from './supporters-types';

export default function Supporters({ supporters }: SupportersProps) {
  return (
    <div className="flex flex-col">
      <div className="flex w-full flex-row flex-wrap gap-y-6">
        {supporters.map(({ name, href, logoUrl }) => (
          <div key={href} className="shrink grow-0 basis-2/4">
            <a href={href}>
              <img src={logoUrl} alt={name} className="max-h-12" />
            </a>
          </div>
        ))}
      </div>
    </div>
  );
}
