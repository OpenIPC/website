import MainButton from '../../ui/buttons/main-button/Main-button';
import icons from '../../../assets/icons/ui';
import { useState, useEffect } from 'preact/hooks';
import Paragraph from '../paragraph/paragraph';

const { Play, Pause, Refresh } = icons;
let rafTimerID: number;
let rafFPSID: number;
let offset: number;
let timeStamp: number;
let counter = 0;

export default function HighResTimer() {
  const [ play, setPlay ] = useState<boolean>(true);
  const [ time, setTime ] = useState(0);
  const [ fps, setFps ] = useState(0);

  function loop() {
    rafTimerID = window.requestAnimationFrame(() => {
      setTime(Math.ceil(performance.now() - offset));
      loop();
    });
  }

  function fpsLoop() {
    rafFPSID = window.requestAnimationFrame(() => {
      counter++;
      if (performance.now() - timeStamp >= 1000) {
        setFps(counter);
        counter = 0;
        timeStamp = performance.now();
      }
      fpsLoop();
    });
  }

  // Starts the two rAF loops once; they re-arm themselves and the cleanup
  // cancels them. Re-running this on every render would start a new pair.
  useEffect(() => {
    offset = performance.now();
    timeStamp = performance.now();
    loop();
    fpsLoop();
    return () => {
      window.cancelAnimationFrame(rafTimerID);
      window.cancelAnimationFrame(rafFPSID);
    }
    // eslint-disable-next-line @eslint-react/exhaustive-deps -- see above
  }, []);

  function controlBtnClickHandler() {
    if (play) {
      window.cancelAnimationFrame(rafTimerID);
    } else {
      offset = performance.now() - time;
      loop();
    }
    setPlay(!play);
  }

  function refreshBtnClickHandler() {
    offset = performance.now();
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
