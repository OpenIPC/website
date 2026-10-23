---
title: Firmware with no camera on the desk, and OpenIPC takes on recorders
date: 2026-10-23
summary: "Our emulator now runs 69 machines across six vendors and boots their own neural networks, a new project brings OpenIPC to video recorders, and a camera can read your utility meters and find dust on its own sensor glass."
author: OpenIPC team
---

## You can develop firmware without owning the camera

Our emulator outgrew its name. Besides HiSilicon it now holds Xiongmai, Goke,
Anyka, SigmaStar and Novatek — 69 machines — and the project is renamed from
`qemu-hisilicon` to [qemu-bench](https://github.com/OpenIPC/qemu-bench). It
boots unmodified firmware, ours and the vendor's alike, to a full Linux
userspace.

This week the emulated Novatek got as far as running the vendor's own neural
networks. A classifier names what it sees; the stock detectors find people,
vehicles and faces; another network finds the cat and the dog; and face
recognition sorted the standard sample photographs, the ones everybody tests
this on, back into the right people. Real audio arrived too, along with MIPI
DSI panels and H.264 and H.265 video on the NT98332. And a new SigmaStar
SSC375 machine runs a Dahua camera dump that serves video to its own RTSP
server.

The vendors published none of this; it is all reverse-engineered. This is what
we build [digital twins](/digital-twins) for: the model has to match a live
camera frame for frame, and that is checked automatically on every push rather
than promised in a report.

## A new project: vigil, for video recorders

OpenIPC is no longer only about cameras. [vigil](https://github.com/OpenIPC/vigil)
is open software for video recorders on cheap hardware. It shows cameras on
HDMI in a two by two view and writes them to disk: network cameras over RTSP or
NETIP, H.264 and H.265, recorded without re-encoding, and decoded in hardware
for display wherever the board has a decoder. On recorders with analogue inputs
those inputs are shown and recorded too. Recording goes into segments that each
play on their own, and the oldest are deleted when the disk fills up.

The Hi3536DV100 is verified on real hardware, the RK3588 is supported, the
Hi3520DV200 is still experimental, and any Linux host can record.

## A camera can read your utility meters

Point it at a water, gas or electricity meter, show it where the digits are,
and it reports the number instead of a picture of it. The rolling drums are
read by a small neural network on the camera's own CPU, so nothing leaves the
camera and no cloud is involved. The little red pointer dials that water meters
use for litres are read from the direction of the needle, and add the digits
below the last drum.

Readings sit on `/metrics` and `/api/v1/meters` and refresh every minute, so
any monitoring system already scraping the camera can chart consumption or
raise an alarm on a leak. If somebody moves the camera it notices and says so,
rather than going on to read a patch of wall. Electronic displays are not read
yet.

## A camera can find the dust on its own sensor glass

A speck sits a fraction of a millimetre in front of the focal plane, so it
never looks like a speck: the lens draws it as a soft blot a few dozen pixels
across that takes the same share of the light away and never moves with the
scene. Most people never notice one for years, until something stretches the
picture — a strong dehaze, a contrast boost, a grey sky.

The camera now hunts for them itself, from the live scene, with no test chart
and nobody standing in front of it. It can also tell dust on the infrared
filter from dust on the cover glass: it moves the filter and watches what moves
with it. And it answers the question that matters after a clean — is the dirt
gone, or did you just move it around?

## The kernel and the filesystem moved into one partition

The flash used to hold them in fixed slots, and each image had to fit its own
even when the room next door sat idle. They are back to back now, and only
their sum has to fit.

What that buys on a Goke V300, measured on today's build. On a camera with 8 MB
of flash the lite system takes 5099 KiB against a 5 MiB slot — 21 KiB from the
ceiling. One more package and the build would simply have stopped fitting. The
new layout has no such ceiling, and the room for settings grew from 704 KiB to
832 along the way.

On a 16 MB camera the difference is larger. The ultimate system takes 6775 KiB
against a 10 MiB slot, so 3.4 MiB sat there dead, with nowhere to give it. It
goes to settings now: 3776 KiB before, 7360 after, very nearly double.

And the image that comes out is one any U-Boot can boot, the factory one
included, which means you can install OpenIPC without touching the bootloader.

## Also

- **Novatek.** Snapshots, `/mjpeg` and `/video.mp4` work on these chips now,
  and the stream stopped dying when the settings are reloaded. We also wrote a
  step-by-step guide to the first install from the vendor's own bootloader.
- **People counting reached the camera itself.** A week ago it was a page in
  the web interface; the lines, the zones, the dwell time and the hourly
  history are counted in the firmware now.
- **Every board in the catalogue has a page of its own** to link to, at
  [/cameras/boards](/cameras/boards). And you can find a board by the name of
  its firmware, which helps when all you have is a file pulled off a camera.
- **The [Club](/club) counts stars per camera** now rather than per report.
  Five reports about the same camera are one camera, not five.
- **We released the person and vehicle detector** for the counting cameras on
  its own, in [xmnpu-models](https://github.com/OpenIPC/xmnpu-models).
- **We opened Xiongmai's encoder.** H.264 on the XM530 is compressed with no
  vendor library at all: our own driver for its Hantro 8290 matches the factory
  module's ioctl ABI call for call, so the firmware does not notice the
  substitution. It is in
  [openxiongmai](https://github.com/OpenIPC/openxiongmai).
- **We kept tidying up the Xiongmai support.** On the XM510 the infrared filter
  is driven by the SoC's own block rather than majestic's night code, the
  watchdog works the way it does on every other chip, and the Pins page is
  drawn from ipctool's pad table so it stops offering pads the kernel already
  holds.
- **Goke boards got a memory-saving mode** for the main stream, and snapshots
  stopped reserving a frame for themselves. Wide sensors now fit boards with
  64 MB.
