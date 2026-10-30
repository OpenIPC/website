---
title: An open decoder for China's video standard, and an open ISP for Xiongmai
date: 2026-10-30
summary: "Cameras encode SVAC 3.0 now and we wrote the decoder for it, faster than the vendor's; the XM510 got an open image pipeline whose white balance beats the factory one; and five more chip makers arrived in the emulator."
author: OpenIPC team
---

## A camera can now record in SVAC 3.0, and we wrote the decoder

SVAC is China's national video standard for security systems, and its third
edition is not even ratified yet — it is a public-comment draft. Cameras on the
Hi3516CV6xx encode it now and serve it over RTSP, and
[libsvac3d](https://github.com/OpenIPC/libsvac3d) takes that stream apart. It
plugs into FFmpeg, so anything can play it.

The decoder is written from the draft's text. Where that text disagrees with
what real streams need, `docs/errata.md` records what the decoder does instead
and why. Every picture of the test corpus decodes bit for bit as the reference
decoder does.

It is also faster than the vendor's decoder. On one thread it is ahead on every
clip, and on four the gap opens up: ours takes 0.95 s on the heaviest clip
against the vendor's 1.40, and 1.59 s against 1.76 on a 2560x1440 one. Inside
are hand-written SIMD kernels for x86 and ARM, an entropy decoder of our own,
and several pictures decoded at once — and whatever the thread count, the bits
that come out are identical.

We also measured what SVAC3 buys in compression, on one chip and the same
clips. The answer is sobering: against H.265 it saves between 0 and 6%, which
puts it in the same class. Choose it to comply with Chinese requirements, not
to save bandwidth. For comparison, H.264 asks for 10-21% more than H.265 on
those clips.

## An open ISP for the XM510, and its white balance is better than the factory one

The ISP is the part which turns the raw numbers off the sensor into a picture,
and on Xiongmai it was closed. There is a daemon of our own now: auto-exposure,
auto white balance, noise reduction and sharpening by gain, suppression of the
banding mains lamps leave, calibrated colour matrices, and a framework for
sensors with six already in it. Verified on two live boards: the lab XM510 with
a JX-H62, and a 53X13 board with an AR0130.

The white balance turned out more accurate than the factory one. In an ordinary
room in daylight the vendor's drifts cyan: it takes dim wood, a beige and
orange chair for grey, sets its gains by them, and the real grey patches of a
colour chart end up at R/G 0.35. We raised the brightness above which a pixel
counts as grey at all and now count only the bright ones; the whole-frame result
then lands within 5% of the chart's grey. And when there are fewer than three
hundred bright grey pixels the ISP holds the gains it has rather than hunting —
otherwise, in the first frames before exposure has brought the picture up, it
would fall into the same cyan trap itself.

How it was done is the interesting part: the factory ISP's behaviour was traced
in our own emulator. The digital twin has stopped being a demonstration and
become the tool you reverse-engineer with. It is all in
[openxiongmai](https://github.com/OpenIPC/openxiongmai).

## Five new makers in the emulator

Allwinner, Fullhan, Grainmedia, more Xiongmai and SigmaStar all arrived in
[qemu-bench](https://github.com/OpenIPC/qemu-bench) at once. Not merely "the
kernel boots", either: a YI camera dump on an Allwinner V831 comes up off its
own flash with networking, an SD card and a coded picture; a retail camera dump
on a SigmaStar MSC313E serves H.264 over RTSP; and Sofia on the XM530V200,
XM533 and XM535 serves H.265 to the same place.

## The unified bootloader repository took in Ingenic

That is a different architecture, MIPS, and the T40 and T41 are the first to
build, NAND images included. Images for the XM V200 series landed in
[OpenIPC/u-boot](https://github.com/OpenIPC/u-boot) too.

## The line drawn over the video became programmable

It used to take the time and a name. It now takes any value the camera knows
about itself — the die temperature, the shutter, its name, its address, its
uptime, a meter reading. Your own value goes in a file and the camera draws
whatever is in it, so anything a script on the camera can write can go on the
picture. The web interface lays all of it out as buttons, and the camera itself
supplies the list of what is available.

## Also

- **A pan/tilt head has presets.** Store a position, go back to it, delete it —
  from the pad in the browser, over ONVIF and over DVRIP.
- **Meter reading reached a working state.** The reading can be drawn straight
  onto the video as a line of its own. The camera waits out a drum mid-carry
  instead of writing nonsense at that moment, the count survives a restart, the
  window adjusts itself if the camera is turned a little, and what the meter is
  busy with shows up on `/metrics`.
- **Image stabilisation works on SigmaStar** now, and its effect is measured
  off the video rather than asserted.
- **The [board catalogue](/cameras/boards) grew by several hundred entries**:
  Anjoy Vision's own archive and 59 more modules, Haixin Weishi's modules, the
  CamHi catalogue. Plus detailed notes from the community — Tino Darkness
  worked through 46 Anjoy boards, with photographs and pin-1 markings.
- **You can merge two of your own [Club](/club) accounts** into one if you
  signed in through Telegram one day and GitHub the next.
- **The GK7201V200 is brought up to where the GK7205V200 is**, and openingenic
  fixed the ISP accessors on the T30.
- **OpenIPC has been an Associate Member of the Linux Foundation** since August
  last year, which the footer of every page now says.
