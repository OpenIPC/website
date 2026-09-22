import type { ComponentChildren } from 'preact';

interface HeaderProps {
  children?: ComponentChildren,
}

export default function Header ({children}: HeaderProps) {
  return (
    <header className="
      flex flex-col items-center bg-brand-blue px-0
      md:px-4
    ">
      { children }
    </header>
  );
}
