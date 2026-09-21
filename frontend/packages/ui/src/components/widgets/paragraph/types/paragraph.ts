import type { FunctionComponent } from 'preact';

export type ComplexParagraph = {
  h: string,
  p: string,
  dl?: boolean,
  icon?: FunctionComponent,
}

export type ParagraphProps = {
  content: string | ComplexParagraph,
  size?: 'big' | 'normal' | 'small',
}
