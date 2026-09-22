import {ParagraphProps} from './types';

export default function Paragraph ({content, size}: ParagraphProps) {
  const text = typeof content === 'string'
    ? content
    : content.p;

  const split = (text: string) => {
    return text.split(/(\[.*?\]\(.*?\))/ig);
  };

  /**
   * Paragraph content is markdown-ish text from a data file or a CMS, and the
   * URL inside [text](url) goes into an href. Anything but an ordinary
   * document link is dropped: `javascript:` in that position runs on click,
   * and `data:` can carry a document of its own.
   */
  const safeHref = (url: string): string | undefined => {
    // Browsers ignore control characters when resolving a URL, so strip them
    // before deciding rather than after.
    const trimmed = [...url]
      .filter((c) => { const n = c.charCodeAt(0); return n > 0x1f && n !== 0x7f; })
      .join('')
      .trim();
    if (trimmed === '') return undefined;
    if (/^\/\//.test(trimmed)) return undefined;              // protocol-relative
    if (/^(https?:|mailto:|tel:)/i.test(trimmed)) return trimmed;
    if (/^[a-z][a-z0-9+.-]*:/i.test(trimmed)) return undefined; // any other scheme
    return trimmed;                                            // relative, #, ?
  };

  const getTextLink = (chunk: string) => {
    return {
      text: Array.from(chunk.matchAll(/\[(.*?)\]\((.*?)\)/gi))[0][1],
      link: Array.from(chunk.matchAll(/\[(.*?)\]\((.*?)\)/gi))[0][2],
    };
  };

  const pText = (text: string) => {
    return split(text).map((chunk, i) => {
      if ((/^\[.*?\]\(.*?\)$/ig).test(chunk)) {
        const { text, link } = getTextLink(chunk);
        const href = safeHref(link);
        if (href === undefined) return text;
        return (
          // eslint-disable-next-line @eslint-react/no-array-index-key -- chunks of one string, in order, never reordered
          <a key={i} href={href} className={`text-brand-blue underline`}>
            {text}
          </a>
        );
      } else {
        return chunk;
      }
    });
  };

  type Sizes = NonNullable<ParagraphProps['size']>;
  const getPStyle = (size: Sizes | undefined) => {
    const fab: Record<Sizes, () => string> = {
      big: () => 'text-lg',
      normal: () => 'text-base',
      small: () => 'text-sm',
    };
    return size
      ? fab[size]()
      : fab['normal']();
  };

  function getHStyle (size: Sizes | undefined) {
    const baseStyle = 'font-bold';
    const fab: Record<Sizes, () => string> = {
      big: () => `text-xl ${baseStyle}`,
      normal: () => `text-base ${baseStyle}`,
      small: () => `text-sm ${baseStyle}`,
    };
    return size
      ? fab[size]()
      : fab['normal']();
  }

  const iconDtStyle = () => 'flex flex-row items-center gap-x-2 text-base font-bold *:w-[33px] *:h-[22px]';

  const hStyle = getHStyle(size);
  const pStyle = getPStyle(size);

  if (typeof content === 'object' && Object.hasOwn(
    content,
    'icon',
  )) {
    const {icon: Icon} = content;
    return (
      <>
        {typeof content === 'object' && (content.dl
          ? <dt className={iconDtStyle()}>{Icon && <Icon />}{content.h}</dt>
          : <h3 className={hStyle}>{content.h}</h3>)}
        {typeof content === 'object' && (content.dl
          ? <dd className={pStyle}>{pText(text)}</dd>
          : <p className={pStyle}>{pText(text)}</p>)}
      </>
    );
  }

  return (
    <>
      {typeof content === 'string' && <p className={pStyle}>{pText(text)}</p>}
      {typeof content === 'object' && (content.dl
        ? <dt className={hStyle}>{pText(content.h)}</dt>
        : <h3 className={hStyle}>{pText(content.h)}</h3>)}
      {typeof content === 'object' && (content.dl
        ? <dd className={pStyle}>{pText(text)}</dd>
        : <p className={pStyle}>{pText(text)}</p>)}
    </>
  );
}
