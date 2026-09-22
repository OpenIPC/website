import { FunctionComponent } from 'preact';

type MainButtonProps = {
  size: 'xs' | 's' | 'm' | 'l',
  disabled?: boolean,
  caption?: string,
  type?: 'button' | 'submit' | 'reset',
  Icon?: FunctionComponent,
  /**
   * Spoken name. Required in effect for an icon-only button: without a
   * caption there is nothing for a screen reader to announce, which is what
   * HighResTimer's play, pause and reset controls were.
   */
  label?: string,
  clickHandler?: () => void,
}

export default MainButtonProps;
