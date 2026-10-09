---
title: Focusing by ear, and exposures measured in seconds
date: 2026-09-25
summary: "GK7201V200 is supported, a motorised lens can be set from a ladder by listening to the camera, and a page covers exposures of seconds for telescopes and other very dark scenes."
author: OpenIPC team
---

## GK7201V200 is supported

The cheapest ARM part XiongMai puts on a board, at 600 MHz. Builds for it are
in CI with the rest.

## Focusing by ear

A camera with a motorised lens can be focused from the web interface and can
focus itself. The part worth having is neither: an installer up a ladder has
no spare hand for a phone, so the sharpness reading becomes a sound.

The beeps come faster and higher the closer the picture is to the best it has
been. Go past the sharpest point and you get a low note; come back and it
turns into one held tone, which is the signal to stop turning. A rising
two-note chime means a new and clearly sharper point has been found, so keep
going.

You can also point the sound at part of the frame rather than all of it — a
plate under a lamp, a doorway across a yard — and the camera outlines the
cells of its focus grid that the rectangle covers.

The page also covers the lens controls, what an autofocus pass actually does,
and the calibration tool in the raw editor's Focus tab. Every screenshot comes
from one camera: a Hi3516EV300 with an IMX335 and a XiongMai motorised zoom
block.

## Exposures measured in seconds

For a telescope, an X-ray screen, a fluorescence rig or a night sky. One rule
governs every setting on the page: **a frame cannot be exposed for longer than
it lasts.** There is no long-exposure mode, only a slow camera, so a five
second exposure means a sensor running at one frame every five seconds.

The trap that costs people an afternoon is where that rate comes from. It is
not `Isp_FrameRate` in the sensor profile; it is the stream frame rates, and
they apply whether or not anything is watching. Leave the second stream at its
default of 15 and exposure stops at 66 ms however high you set it. Both
streams have to come down. Below one frame per second the streams cannot go,
and a fraction in the sensor profile takes over. On an IMX335 at 5 MP the
ceiling is about 7.7 seconds.

The page also settles the usual warning about heat. At room temperature in a
light-tight box, dark current on this sensor was too small to measure: the
frame would need more than an hour and a half to fill from it. What does grow
is hot pixels, and even at a quarter of an hour they are under one percent of
the frame — and they sit in the same place every time, so a dark frame
subtracts them exactly.

## Tuning the picture

Two pages, for the two ends of it.

**Automatic image tuning** watches the picture the camera is sending and moves
up to four settings to follow it, because settings chosen on installation day
were chosen for that day's weather. It is on by default from the September
builds, and deliberately undramatic: on a picture that already uses its range
it does nothing.

**Tuning it yourself with the vendor tools** is now written down end to end:
exporting a profile, putting it somewhere that survives a reboot, and baking
it into a firmware image. It also explains the old puzzle where a value you
set comes back on its own — that is the camera moving the same settings while
you work, and it can now be told to keep its hands off for the session.

## Also

- **The IR-cut filter and the lamp each have a mode of their own** now:
  automatic, manual, or left alone. None of the three means "nothing is
  wired", which is worth knowing when a filter sitting open in daylight gives
  you a pink picture.
- **How much this camera has written to the card**, kept on the card itself so
  it travels with it. A large number is mileage, not a diagnosis.
- **Raw frames on SigmaStar and Ingenic T31/T23**, where it used to be
  HiSilicon and Goke only. Off by default on SigmaStar and T23.
- **Smaller things**: the raw editor's screenshots were retaken, all from one
  scene and with a real colour chart, and its tabs go by their current names;
  more SSC377D detail in the GPIO tables; and the table of contents had a
  tidy.
