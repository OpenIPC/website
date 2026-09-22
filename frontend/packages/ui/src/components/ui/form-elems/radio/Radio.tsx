import { useState} from "preact/hooks";
import type { RadioProps } from './radio-types';

export default function<T extends string[]>({ name, defaultChecked, captions, changeHandler }: RadioProps<T>) {
  if ([...(new Set(captions))].length !== captions.length) {
    throw new Error('All captiona must be unique values');
  }

  const [ checked, setChecked ] = useState(captions[defaultChecked]);

  function handleChange(e: Event) {
    if (e.target instanceof HTMLInputElement) {
      setChecked(e.target.value);
      changeHandler(e.target.value);
    }
  }

  return (
    <ul className="
      flex min-h-8 max-w-min flex-row flex-nowrap items-center rounded-sm border
      p-0.5 shadow-md
    ">
      {captions.map((caption) => (
        <li key={caption} className="relative">
          <input name={name} value={caption} id={`${name}-${caption}`} checked={caption === checked} type="radio" onChange={handleChange} className="
            peer absolute opacity-0
          " />
          <label for={`${name}-${caption}`} className="
            flex min-w-24 cursor-pointer flex-col justify-center rounded-sm
            border-0 text-center transition
            peer-checked:bg-brand-blue peer-checked:text-white
            peer-[:not(:checked):hover]:bg-light-blue
          ">
            {caption}
          </label>
        </li>
      ))}
    </ul>
  );
}
