import MainButton from '../../ui/buttons/main-button/Main-button';
import icons from '../../../assets/icons/ui';
import { useState, useEffect, useRef } from 'preact/hooks';
import Paragraph from '../paragraph/paragraph';

const { Play, Pause, Refresh } = icons;

export default function HighResTimer() {
  const [ play, setPlay ] = useState<boolean>(true);
  const [ time, setTime ] = useState(0);
  const [ fps, setFps ] = useState(0);

  // Per instance. These were module-level, so two timers on one page shared
  // one set: either could cancel the other's animation frames or reset the
  // other's clock, and pausing one left the other's loop running untracked.
  const rafTimerRef = useRef<number>(0);
  const rafFpsRef = useRef<number>(0);
  const offsetRef = useRef<number>(0);
  const timeStampRef = useRef<number>(0);
  const counterRef = useRef<number>(0);

  function loop() {
    rafTimerRef.current = window.requestAnimationFrame(() => {
      setTime(Math.ceil(performance.now() - offsetRef.current));
      loop();
    });
  }

  function fpsLoop() {
    rafFpsRef.current = window.requestAnimationFrame(() => {
      counterRef.current++;
      if (performance.now() - timeStampRef.current >= 1000) {
        setFps(counterRef.current);
        counterRef.current = 0;
        timeStampRef.current = performance.now();
      }
      fpsLoop();
    });
  }

  // Starts the two rAF loops once; they re-arm themselves and the cleanup
  // cancels them. Re-running this on every render would start a new pair.
  useEffect(() => {
    offsetRef.current = performance.now();
    timeStampRef.current = performance.now();
    loop();
    fpsLoop();
    return () => {
      window.cancelAnimationFrame(rafTimerRef.current);
      window.cancelAnimationFrame(rafFpsRef.current);
    }
    // eslint-disable-next-line @eslint-react/exhaustive-deps -- see above
  }, []);

  function controlBtnClickHandler() {
    if (play) {
      window.cancelAnimationFrame(rafTimerRef.current);
    } else {
      offsetRef.current = performance.now() - time;
      loop();
    }
    setPlay(!play);
  }

  function refreshBtnClickHandler() {
    offsetRef.current = performance.now();
    setTime(0);
  }

  return (
    <div className="flex max-w-full flex-col items-center">
      <p className="text-[10vw] leading-none font-bold">{time}</p>
      <p className="text-[4vw]">{fps} fps</p>
      <div className="mt-3 mb-5 flex gap-x-2">
        { play
            ? <MainButton size='xs' Icon={Pause} type='button' clickHandler={controlBtnClickHandler} />
            : <MainButton size='xs' Icon={Play} type='button' clickHandler={controlBtnClickHandler} />
        }
        <MainButton size='xs' Icon={Refresh} type='button' clickHandler={refreshBtnClickHandler} />
      </div>
      <Paragraph content="Time shown in milliseconds" size="small" />
    </div>
  );
}
