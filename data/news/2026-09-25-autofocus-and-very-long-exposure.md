---
title: Focusing by ear, and exposures measured in seconds
date: 2026-09-25
summary: "GK7201V200 is supported, a motorised lens can be set from a ladder by listening to the camera, and a page covers exposures of seconds for telescopes and other very dark scenes."
author: OpenIPC team
---

What turned up this week:

- **GK7201V200 is supported** — the cheapest ARM part at 600 MHz on XiongMai
  boards.

- **A new page on motorised lenses.** The camera focuses itself and the lens
  can be driven from the web interface, but the interesting part is focusing
  by ear. An installer up a ladder has no time to look at a phone, so the
  camera beeps: the sharper the picture, the faster and higher the beeps. Go
  past the best point and you get a low note. Come back and it is a steady
  tone, which means stop turning. You can listen to part of the frame rather
  than all of it: a plate under a lamp, a doorway across a yard.
  <https://github.com/OpenIPC/wiki/blob/master/en/autofocus.md>
  <https://github.com/OpenIPC/wiki/blob/master/en/autofocus.md#focusing-by-ear>

- **A new page on shooting in almost complete darkness**: a telescope, an
  X-ray screen, a night sky. It all rests on one rule: the exposure cannot be
  longer than the frame itself. There is no separate long-exposure mode, there
  is a slow camera. Want a five second exposure — the camera has to produce
  one frame every five seconds.

  Hence the main trap, the one that costs people an evening. The sensor's
  speed is set by the stream frame rates, not by the sensor profile, and both
  streams have to be slowed. Leave the second at its factory fifteen frames
  and the exposure stops at 66 milliseconds however high you set it, while the
  camera looks broken. Below one frame per second the streams cannot go: past
  that a fraction goes in the sensor profile. On an IMX335 at 5 MP the limit
  is about 7.7 seconds.

  Heat was measured too, and it barely matters: at room temperature the
  thermal noise is lost in the ordinary noise. What does grow is the number of
  hot pixels, the ones that glow on their own. Even those come to under one
  percent of the frame in a quarter of an hour, and they are easy to remove:
  a hot pixel is always in the same place, so a frame shot with the lens
  capped subtracts it.
  <https://github.com/OpenIPC/wiki/blob/master/en/very-long-exposure.md>

- **The camera adjusts the picture to the weather.** It was set up on
  installation day, for that day's weather, and then fog arrives, or the sun
  drops low and the frame holds bright sky and deep shade at once. The camera
  now watches what it is sending and moves up to four settings. On by default
  since the September builds. It works quietly: if the picture is fine, it
  touches nothing.
  <https://github.com/OpenIPC/wiki/blob/master/en/automatic-image-tuning.md>

- **Tuning the picture with the vendor tools, written out end to end**: how to
  export a profile, where to put it so it survives a reboot, and how to bake
  it into firmware. It also explains an old oddity: you set a value and it
  comes back. That is the camera moving the same settings at the same time as
  you. It can now be told not to interfere while you work. It saves nothing
  while it holds off, so take the result yourself.
  <https://github.com/OpenIPC/wiki/blob/master/en/image-quality-tuning.md>

- **The IR-cut filter and the lamp each have their own mode now**: automatic,
  by hand, or leave it alone. "Leave it alone" does not mean "not wired": the
  pins stay configured, and only who decides changes. Worth remembering when
  looking for a cause: a filter sitting open in daylight turns the picture
  pink, and the mode may be at fault rather than the wiring. There is also a
  new pause on the switch, for anyone who was seeing a colour frame slip
  through.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#who-moves-the-filter-and-the-lamp>

- **The camera counts how much it has written to the card** and shows it on
  the card page. The counter lives on the card itself and travels with it into
  another camera. A large number means nothing bad on its own; it is mileage.
  <https://github.com/OpenIPC/wiki/blob/master/en/sd-card-diagnostics.md#written-by-this-camera>

- **A raw frame straight off the sensor**, with no processing, is now served
  by SigmaStar and by Ingenic T31 and T23 as well. It used to be HiSilicon and
  Goke only. On SigmaStar and T23 it is off by default; the pages say why and
  how to turn it on.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#on-sigmastar>
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#on-ingenic-t31-and-t23>

- **Smaller things**: the raw editor's screenshots were replaced — they are
  now from one scene and with a real colour chart, and the tabs go by their
  new names. More SSC377D detail was added. The links in the table of contents
  had a tidy.
  <https://github.com/OpenIPC/wiki/blob/master/en/raw-editor.md>
  <https://github.com/OpenIPC/wiki/blob/master/en/gpio-settings.md>
