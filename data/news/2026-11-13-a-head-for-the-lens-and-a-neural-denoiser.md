---
title: A head for the lens, and a camera that denoises with a neural network
date: 2026-11-13
summary: "A camera can now drive a zoom lens and the pan/tilt head under it at the same time, a Hi3516CV610 denoises with its NPU at a cost we measured, the meter reader catches its own misreads, and handover between radio cells works across three cells at once."
author: OpenIPC team
---

## A camera can drive a zoom lens and a pan/tilt base at once

A camera can now run a zoom block and the head it sits on at the same time,
even when the two are not on the same wire. `ptz.head` names the base's
protocol — Pelco-D today — and `ptz.headPort` says where to find it: the
lens's own serial port, a tty of its own, or — for a base behind a serial
gateway on the network — a `tcp://` address with the gateway's host and
port. Sony VISCA zoom blocks are
a supported lens now, and such a block reports its own gain back to the
camera as an ISO, so the picture settings know what the lens is doing.

Tried on a XiongMai N51813L: a VISCA lens on UART0, a Pelco-D base behind a
socat gateway, both driven from the WebUI pad. Read [the new
page](https://github.com/OpenIPC/wiki/blob/master/en/ptz-head.md) before wiring anything —
XiongMai's CN2 "485" port is 3.3 V TTL, not RS-485, and the page says what
to do about it.

## The camera denoises with a neural network, and we measured what it costs

A Hi3516CV610 with an NPU can now run a denoising network over the picture
before it is encoded, with `isp.aibnr`. [The new
page](https://github.com/OpenIPC/wiki/blob/master/en/majestic-ai-noise-reduction.md) says which
chip variants have the NPU, how to tell from `socmodel`, which model files
the ultimate firmware needs, and how to switch it on.

It also says plainly what you pay. Measured on an IMX662 at 1080p: the NPU
needs about 33 ms per frame, so a sensor running at 30 fps delivers 28, the
NPU sits at 94% busy, and free memory drops from 76 to 44 MB. Cap it with
`isp.aibnrFps: 25` and the NPU drops to 82%. The outermost 8 pixels on each
side are cropped, because the denoiser leaves a faint pattern there. At five
megapixels the NPU would manage about 10 frames a second.

What the page does not claim: how much better a dark, detailed scene looks.
That has not been measured yet, and it says so.

## The white lamp got controls of its own

The white light is no longer tied to whatever the infrared pins do.
`nightMode.whiteLightPin` puts it on a plain GPIO pin, and on SigmaStar it
dims through the kernel's own PWM driver, with its frequency, limits and
curve set separately from the IR lamp's. `colorToGray` decides whether the
picture goes grey when the lamp is white. The keys are in [the streamer's
page](https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md).

The WebUI followed: a White switch next to the others, the IR-cut switch now
says which way is on, the lamp's pin appears on the pin map, and the four
lamp switches fit on a phone.

## The meter reader notices when it has misread a digit

Last week the camera learnt to read a meter's blinking pulse LED. The lab's
Energomera reads its 1 and 2 as a 7, which is exactly the problem that
breaks a reading — so the camera now checks the digits against the blinks. A
reading further ahead than the pulses allow is ignored however often it
repeats, the reading never goes backwards while the blinks are being
counted, and a drum stuck on a digit counts as a stall while the pulses
carry the number on. Two new metrics report both cases, and
[majestic-meters](https://github.com/OpenIPC/wiki/blob/master/en/majestic-meters.md) has them.

## You can talk through the camera from an XM recorder

A camera on an XM NVR or in XMEye now carries audio both ways, so the
operator can answer whoever is standing in front of it.

## Handover works, on three cells at once

Last week this section said the scheduler was still being written and that
crossing between cells was the last step. That step is taken. The whole
chain is closed on the PCIe cards — a scheduled single cell, reliable
delivery, admission, one clock across cells, coordinated scheduling, and now
make-before-break handover.

Three cells now run on the bench at once, on three channels, and a device
moves between them on its own measurements: it reports what it hears, the
controller picks the strongest neighbour, and the device arrives in its slot
in the new cell 99.9 to 100% of the time, with its clock continuous. The
part that made it work is the suspend gap — a planned deaf moment in which a
device listens to a cell it is not attached to. Without those gaps, two of
the six crossings measured nothing at all.

New hardware too: the RTL8822BE, RTL8822CE and RTL8852BE PCIe cards.

Still a bench, and still honest about it: the remaining milestone is a
moving device over a real path — fading, several devices, and a walk in the
open air. [The tracking
issue](https://github.com/OpenIPC/devourer/issues/254) is where it happens.

## Your flash dump no longer takes your Wi-Fi password with it

The [board catalogue](/cameras/boards) is built from dumps people send in. A
dump holds more than a board's identity, so the site now strips Wi-Fi
passwords, Gwell P2P keys and cloud credentials out of a report before it is
stored, and a dump that would have been published is kept private when it
carries its owner's secrets. Photographs are thumbnailed and decoded within
a memory budget instead of whatever the phone sent.

## Also

Thirty-two repositories saw work this week. Three of them are above —
**devourer**, **majestic-webui** and the **wiki** — and here are the rest.

- **qemu-bench** built the Hi3516CV610 twin: the gen-5 encoder, VI and VPSS
  online, the ISP statistics in V5's layout so the streamer's exposure and
  white balance converge, and 29 of the chip's accelerator operations as NPU
  tasks. White balance now converges in the older twins too, a scene can be
  placed in front of a camera and measured frame by frame, and a Xiongmai V1
  flash image boots.
- **microsnander** grew up as a flash programmer: 143 more NOR chips and 32
  more NAND chips, a chip not in the table found through its SFDP table, NOR
  over 16 MB, factory bad blocks kept or skipped, a dump saved only when two
  reads agree, and the CH347 writing about 30% faster.
- **majestic** also brought Fullhan up to the others — snapshots, live
  sliders, AE ceilings, an OSD, hue and saturation through the colour
  matrix, manual white balance — and gave gen-1 HiSilicon parts crop and
  plugins, T31 rotation, and slow shutter that follows the vendor's rule.
- **linux** carried Anyka EV200 and EV300 capture, the Fullhan FH8626 ARC
  coprocessor, the SigmaStar split NOR layout, and PWM pad fixes.
- **firmware** built the ATBM Wi-Fi drivers from source, gave SigmaStar the
  split NOR image, added a neo variant for the hi3520dv200, and tuned a long
  list of sensors against stock cameras.
- **u-boot** took SigmaStar to a common layer with CI on every die, NOR and
  NAND, and booted Allwinner V831/V833 on real silicon.
- **openhisilicon** recovered six hi3516cv6xx sensor drivers and checked
  them against Sony's datasheets.
- **ipctool** learnt a read-only GPIO dump of every pin's mux and level, die
  temperature on three Novatek parts, and the AR0230, IMX662 and Anyka
  parts.
- **builder** added seven boards, among them the Tapo C210 v2, an Amcrest
  S3L and the HeimVision PE3020-W.
- **majestic-af** gained the VISCA actuator and the head port; **motors**
  gained a bit-banged Pelco-D sender for debugging one.
- **website** also fixed board filters and firmware download pooling;
  **divinus**, **viewer**, **decoder**, **PixelPilot_rk** and **hevc-wasm**
  took player and overlay fixes; **vectis** and **camex** got console
  polish; **waybeam** drove an IMX335 and IMX662 on the CV610;
  **openxiongmai** tuned SC5332 and SC5235 gain; **coupler**, **defib**,
  **openingenic**, **u-boot-ingenic**, **atbm-wifi**, **atbm_60xx**,
  **rda-wlan** and **sbc-groundstations** carried board, driver and build
  work.
