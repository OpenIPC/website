import { H2Props } from '../types';

export default function H2(props: H2Props) {
  const { content } = props;
  return (
    <h2 className="text-2xl font-normal">{content}</h2>
  );
}
