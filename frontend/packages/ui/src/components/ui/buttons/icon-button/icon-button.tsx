import { ComponentChildren } from 'preact';

interface IconButtonProps {
  children: ComponentChildren;
  clickHandler: (evt: MouseEvent) => void;
  withBorder?: boolean;
  /** Defaults to 'button'. A bare <button> inside a form submits it. */
  type?: 'button' | 'submit' | 'reset';
}

export default function IconButton(props: IconButtonProps) {
  const { withBorder, children, clickHandler, type = 'button' } = props;
  return (
    <button type={type} class={`
      box-border
      ${withBorder ? 'border' : ''}
      rounded-md border-light-blue
      *:size-[24px]
    `} onClick={clickHandler}>
      {children}
    </button>
  );
}
