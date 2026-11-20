---
title: A new silicon vendor, and a camera you can bring up from your phone
date: 2026-11-20
summary: "OpenIPC now builds firmware for two Axera chips, a tunnel built into three of our apps reaches a camera with no address of its own, the Live page gained a full set of steering controls, and a HiSilicon recorder chip came up with SATA so ordinary hard drives work."
author: OpenIPC team
---

## A new silicon vendor: Axera

OpenIPC now builds firmware for two Axera chips — the AX620E and the
four-core AX620U. Both start on our own bootloader, built from source: all
that is left of the vendor's is the small piece the boot ROM needs to bring
up the RAM. The kernels are the vendor's, with Axera's drivers ported into
them, and the whole vendor media stack comes up on top. If the firmware will
not boot, a camera on either chip can be recovered over the network or from
an SD card.

What there is not yet: a working camera. All of this was checked in the
emulator, we have not run it on a live board, and majestic does not stream
on Axera — it has no backend for these chips. Only the stock application
sends video, which is how we know the hardware itself is fine.

## You can bring a camera up, and watch it, from your phone

We have camex: a tunnel that reaches a camera even when it has no address of
its own on the internet. It used to be a program you ran separately. It can
now be built straight into an app, and three of ours did exactly that.

vectis is a small adapter board — it powers the camera and brings out its
console. It now has an Android app that runs even on a TV box without root,
and the console opens as an ordinary web page, behind a password if you want
one.

microsnander reads and writes flash chips. Its app learnt to work through
the tunnel: pull a dump, look through it byte by byte, and flip a bit in the
chip's register with one tap.

The decoder plays camera video on Android and on televisions. It gained a
Network window, which connects to the camera through the same tunnel.

None of them needs port forwarding or a public address.

## Steering a camera got comfortable, and autofocus stopped clicking

The Live page in the web interface gained a whole set of controls: draw a
box on the picture and the camera turns to it; save a position, or a whole
scene, and come back to it with one tap; there is iris control, a gallery of
the frames where autofocus found its sharpness, and triggers that refocus
the camera on an event.

While you are steering, settings are no longer written to flash on every
nudge: the write happens once, when you let go. On a chip whose settings
live in NOR, that noticeably extends the memory's life.

Autofocus itself got quieter: it no longer clicks, no longer stalls, picks
the right point of sharpness, and refocuses after the camera switches
between day and night. If there is nothing in the frame to focus on, it now
says honestly that it could not — it used to report success. On Anyka chips
a pan/tilt head can be driven from plain processor pins, with no separate
controller.

## A recorder with real disks

The Hi3520D V100 is a chip for a video recorder rather than a camera, and
this week it came up far enough to work as one: clocks and reset, USB, a
real-time clock and the crypto engine all run, and — the part that matters —
so does SATA, which means ordinary hard drives can be plugged into it. The
first board this was checked on is a Juan NVR0408, for four cameras.

## The camera stops guessing what its own filesystem is

To mount the root partition, the kernel used to try every filesystem it
knows, one after another, until one of them took. Now it mounts the one the
partition actually holds, and if the partition cannot be read it tries only
the two types it could possibly be. The fix was written once and ported to
seventeen platforms, from the Goke GK710x to the Hi3519DV500, both SigmaStar
families among them.

## Split NOR is the standard now, not the exception

The firmware wizard on the site says so plainly: on every processor we
publish such images for, the split layout is the standard, and Lite on an
8 MB camera is the split image, because that is the one that fits. This week
the layout reached every Ingenic board, SigmaStar's infinity6c, every `neo`
build, and the mainline Linux 7.0 kernel.

## Goke's GK7206 and Anyka's chips became cameras

majestic now runs on the Goke GK7206V1 family, and with it these cameras got
everything you expect from a camera: neural-network noise reduction, audio,
text on the picture, motion detection, night mode and slow shutter in
auto-exposure. Image tuning can be written as text now, instead of feeding
the camera a binary file from the vendor.

Anyka's EV200, EV300 and the AV100 family came up as well — the camera
captures and encodes through the kernel's own drivers, on the EV300 in H.265
too. And a PE3020-W looks at its own light sensor and decides for itself
that night has fallen.

## Also

Thirty repositories saw work this week, and 411 pull requests were merged.

- **qemu-bench** built a complete digital twin of the Fullhan FH8626V100 —
  from boot to login, with its coprocessor, its frame path and its H.264 and
  JPEG encoders — and added twins for the Hi3518EV200, the Anyka AV130 and
  AV100, the Goke GK7206V11A running Xiongmai's stock firmware, and both
  Axera chips.
- **the wiki** wrote down that [the AltoBeam ATBM6032 Wi-Fi chip works on
  the XM530](https://github.com/OpenIPC/wiki/blob/master/en/wifi-xm530.md) and what the XM711 image
  actually ships, that [Novatek's IMX415 is supported but has to be set by
  hand](https://github.com/OpenIPC/wiki/blob/master/en/install-novatek.md) and that Ctrl-C stops the
  vendor's bootloader, and [which chip sockets are worth
  buying](https://github.com/OpenIPC/wiki/blob/master/en/flash-chip-interfacing.md) for a programmer.
- **firmware** brought up that ATBM6032, added Ingenic T41 boards, released
  the image for the GK7206 family, and tuned a long list of sensors.
- **u-boot** built Axera's bootloader from source with the stock one as a
  fallback, added the Anyka AV100 family, and stops the watchdog at boot on
  every processor.
- **builder** added the Misecu XM-63-AI-50W, HeimVision HM202 and VStarcam
  C23S cameras, an anbiux XM530 board and an Imou whose card slot now works;
  **coupler** added a GK7205V300 20x zoom block and a Rostelecom camera.
- **ipctool** learnt to recognise the Axera chips, tell a T41ZX from a T41A,
  and name the GK7206 family; **defib** read a GK7206's whole 16 MB through
  its boot ROM.
- **nvr** now reports memory sensibly on cameras with under a gigabyte, and
  its dark theme and page layout were put in order.
- **openhisilicon** replaced two vendor blobs with builds from source;
  **openingenic** built the T41 modules; **waybeam** cut memory use on the
  CV610 from 37 to 21 MiB at 1080p30.
- **divinus**, **onvif-tt**, **aviateur** and **motors** carried protocol,
  test and Windows fixes; **sensors** drove SigmaStar's imx335 and sc850sl;
  **smolrtsp**, **smolrtsp-libevent** and **mavfwd** took maintenance work;
  **website** re-staged the whole catalogue, put the board and report pages
  in order, and accepted build reports from two more vendors.
