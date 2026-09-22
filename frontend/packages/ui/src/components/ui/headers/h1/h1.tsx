import { H1Props } from '../types';

export default function H1(props: H1Props) {
  const { content } = props;
  return (
    <h1 className="text-3xl font-bold text-brand-blue">{content}</h1>
  );
}
