import { FunctionComponent } from 'preact';

type MainButtonProps = {
  size: 'xs' | 's' | 'm' | 'l',
  disabled?: boolean,
  caption?: string,
  type?: 'button' | 'submit' | 'reset',
  Icon?: FunctionComponent,
  clickHandler?: () => void,
}

export default MainButtonProps;
