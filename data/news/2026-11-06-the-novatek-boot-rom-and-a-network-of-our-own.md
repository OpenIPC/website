---
title: The Novatek boot ROM opened, and a radio network of our own
date: 2026-11-06
summary: "A bricked Novatek board now comes back over Ethernet because we reconstructed its mask ROM, Xiongmai cameras became proper cameras, a camera reads your power draw off a meter's blinking LED, and cheap Wi-Fi cards turned out capable of the timing a mobile-style network needs."
author: OpenIPC team
---

## We opened the Novatek boot ROM, and a bricked board comes back over the network

[opennovatek](https://github.com/OpenIPC/opennovatek) is a new repository: the
on-chip mask ROM of the NT9856x, reconstructed as reviewable C and ARM
assembly. It is a clean-room job — the ROM image itself is never committed.
Each contributor dumps it from their own camera, and `MANIFEST` pins the hash
the dump has to match.

Why the ROM matters: it is what decides where a camera will look for a loader
when its flash is dead — SPI, an SD card, the network, USB or UART — in what
order, and what it checks before running one. Our
[defib](https://github.com/OpenIPC/defib) and the ROM model in
[qemu-bench](https://github.com/OpenIPC/qemu-bench) were both built from what
this reconstruction documents.

The result: an NT9856x board with a dead flash is recovered over Ethernet
rather than with a soldering iron on the UART. It also turned up why that did
not work before — Novatek's ROM and loader never ask again for a TFTP block
they lost, so defib asks for them instead.

## Xiongmai cameras went from "it boots" to proper cameras

A week ago we wrote an open image pipeline for the XM510, and everything else
has now followed it. On the XM510, XM530 and XM550 the contrast, hue,
saturation, brightness, mirror and flip all change live; motion detection
works, and so does tamper detection — when the camera is covered or turned
away; the suppression of banding from mains lamps reached auto-exposure; and
the factory encoder started obeying our settings for bitrate, rate-control
mode, frame rate, keyframe interval, profile and quality.

A smart keyframe mode arrived, the sub-stream learned to follow the size it is
given, and `/metrics` shows the rate the camera really captures at and how many
frames the encoder dropped.

## A camera looks at your electricity meter and knows what the kettle is drawing

These meters have a lamp that blinks a fixed number of times per kilowatt-hour
— at 3200 impulses that is 0.3125 Wh a blink — so the gap between blinks is the
power being used. The camera counts them and reports consumption second by
second, within 0.35% from 60 W to 6 kW.

It also fixes an old sore point. The drums alone cannot give you power: they
step 0.1 kWh at a time, and the last digit caught between two positions reads
as anything at all. On the lab meter its 8.7 came out as 0, 2, 6, 7 or 8 about
a third of the time each, and the reading sat at .3 for twenty minutes while
the meter climbed to .8. Between the drum's steps the blinks now carry the
reading.

The elegant part is that it costs nothing. The camera does not process frames
to watch a lamp: it reads the auto-exposure statistics the ISP already averages
by zone, every frame, for its own purposes. A blink is the lamp's zone and the
eight around it jumping by a tenth and coming back within a second, while the
sixteen zones around those — the room — move less than a third as much. That
last condition is what stops somebody switching the light on from counting.

## A mobile-style radio network out of cheap routers and Wi-Fi cards

Ordinary Wi-Fi works like this: everyone transmits when they can, and whoever
gets in first wins. At the edge of a cell, where a device hears two access
points at once, they get in each other's way — exactly where a moving robot or
drone can least afford it. And when a device crosses from one access point to
the next, the link goes away for more than a tenth of a second: scan,
re-associate, re-acquire the clock. For live video that is a frozen picture.

We want to hand out the air by schedule instead, the way a mobile network
does: a slot for each device, and the access point decides who gets which. The
first question was whether cheap Realtek cards can do that at all, and this
autumn we finished measuring it. They can. A card can pin its beacon to an
absolute moment without disturbing its own clock, report when a frame actually
left the antenna, acknowledge in hardware, and retune quickly. On an 8821CE one
access point disciplined its beacon to PTP time arriving over Ethernet to
within 0.67 microseconds.

That is the foundation rather than the network. The scheduler on top of it is
what we are writing now, in order: one cell on a fixed grid of slots, then
reliable delivery, then letting new devices in, then one clock shared across
cells, and last the crossing between them, where a moving robot would hold
slots in both cells at once. [The tracking
issue](https://github.com/OpenIPC/devourer/issues/254) carries the order of work and
the measurements behind it.

What it is all for: live video from something that keeps moving, where
ordinary Wi-Fi gives up. A robot in a warehouse, a drone on its rounds,
machinery on a site.
## Compressed swap turned 215 seconds into 11

When a camera runs out of memory, the kernel does not kill anything at first:
it throws out the running programs' code and reads it back from flash, over and
over. On a 64 MB hi3516cv608 a streamer start in that state took 215 and 249
seconds, and the program was killed by the out-of-memory killer in the end.

With zram — a compressed swap living in RAM — the same starts came up in 11 to
12 seconds and nothing had to be killed. A hybrid allocator landed on the same
chip too: a carve-out with a CMA zone beside it.

## Also

Thirty-four repositories saw work this week.

- **microsnander** gained in-system programming for SigmaStar parts and an
  Android app, and took well over a hundred commits.
- **decoder** — support for Lewei cameras, checked on a VISUO drone.
- **viewer** — polish for televisions: the on-screen keyboard, and the player's
  focus and hotkeys while a dialog is open.
- **nvr** — a leak of connections to the camera is fixed, and moving recordings
  no longer eats the backend's memory.
- **divinus** — sensor identification on the AK3918, and discovery on the
  network before an address has been handed out.
- **ipctool** — Ambarella reports its real memory total, and a GC4653 on a
  Hi3516CV610 no longer wedges the bus.
- **coupler** — new boards: the GK7201V200 G3C, Huishi and Vatilon boards, and
  a size check on Anjoy's bootloader.
- **companion** released 0.10.0; **aviateur** and **PixelPilot** got
  ground-station interface fixes.
- **qemu-bench**, **u-boot**, **openhisilicon**, **firmware**, **builder** and
  **linux** carried on with platform and build work.
