import Copyright from '../copyright';
import Socials from '../socials';
import Disclaimer from '../disclaimer';

export default function Footer() {
  return (
    <footer className="flex flex-col items-center justify-start bg-grey-bg px-4">
      <div className="max-w-[1240px] pb-2">
        <div className="
          flex w-full flex-col-reverse items-center gap-y-2.5 pb-3
          sm:flex-row sm:justify-between
        ">
          <Copyright />
          <Socials />
        </div>
        <div>
          <Disclaimer />
        </div>
      </div>
    </footer>
  );
}
