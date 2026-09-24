/**
 * `InstallationHelper#terminal_block`, and the notes that hang under it.
 *
 * Deliberately without the copy button shared/_terminal carries. Every one of
 * these blocks opens with "enter commands line by line", and the reason is in
 * `guarded_flash`: a bootloader that does not understand `&&` runs a pasted
 * line as one command, or runs the write without the erase. One click that
 * puts all of it on the clipboard, on a page where that bricks a camera, is
 * not a convenience.
 *
 * The block wraps rather than scrolls, as _terminal.scss does: a tftpboot line
 * is longer than the column and a reader is about to paste it into a root
 * shell, so being able to read it to its end is the whole point of showing it.
 */
interface Props {
  lines: string[];
  /** Whether the block opens with the do-not-paste line. */
  noPaste: boolean;
  shellLabel: string;
  doNotPaste: string;
}

export default function Terminal({ lines, noPaste, shellLabel, doNotPaste }: Props) {
  return (
    <div class="my-4 overflow-hidden rounded-lg bg-ink font-mono text-left text-[#e7ebf5]" dir="ltr">
      <div class="flex items-center gap-2 bg-ink-2 px-3 py-2 text-xs text-white/55">
        <span class="inline-flex gap-[.3rem]">
          <span class="size-[.55rem] rounded-full bg-white/25" />
          <span class="size-[.55rem] rounded-full bg-white/25" />
          <span class="size-[.55rem] rounded-full bg-white/25" />
        </span>
        <span>{shellLabel}</span>
      </div>
      <pre class="m-0 px-4 py-[14px] text-sm leading-[1.6] whitespace-pre-wrap [overflow-wrap:anywhere]"><code>
        {noPaste && <span class="text-[#d6453d]">{doNotPaste}{'\n'}</span>}
        {lines.join('\n')}
      </code></pre>
    </div>
  );
}
