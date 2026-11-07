---
title: Ambarella S3L is coming to OpenIPC, and we need owners of these cameras
date: 2026-11-07
summary: "OpenIPC firmware now builds for the Ambarella S3L, and on one camera it boots and streams video. Before an image is safe to flash, we need to know which S3L cameras are out there, and one ipctool report from stock firmware tells us."
author: OpenIPC team
---

The Ambarella S3L is a camera chip that Dahua built a large family of IP
cameras on, and those cameras were sold under many other brands. Until this
week, OpenIPC had no firmware for it that gave video. That has changed, and
now we need the people who own these cameras.

## What works

- **The kernel.** The S3L kernel in
  [OpenIPC/linux](https://github.com/OpenIPC/linux), branch `ambarella-s3l`,
  now has overlayfs, so settings survive a reboot.
- **The video stack.** Ambarella's DSP microcode, from the vendor's SDK, ships
  with a loader we wrote ourselves. Majestic, our streamer, is in the image.
- **CI.** [OpenIPC/firmware](https://github.com/OpenIPC/firmware) builds the
  `s3l_ultimate` board on every change. The build stops if the kernel would
  break the vendor's prebuilt video driver.
- **One real camera.** On an Amcrest IP4M-1026E with an OV4689 sensor, a
  contributor's build of this firmware, with that board's own profile, boots
  from power-on: the DSP starts, the camera streams 2688×1512 H.264, and
  settings survive a reboot. The streamer in that run was the contributor's
  own, not Majestic.

## What doesn't, yet

**Please don't flash the S3L image from the nightly build.** It is generic:
it carries no device tree for any particular board, and the one camera we have
tested needs its own. Booting the published image from a camera's flash has
not been tried. Board profiles are what make an image safe for a given camera,
and we can only write them for cameras we know about.

## What we need from you

If you have a camera with an S3L in it, send us one report. It takes a few
minutes, it doesn't change anything on the camera, and it works on the
camera's stock firmware:

1. Get a shell on the camera, over telnet or a serial console.
2. Follow [the report page](/cameras/report). It shows how to fetch and run
   ipctool on stock firmware, which has no curl, and how to send its output.

ipctool recognises Ambarella chips. The report tells us the chip, the sensor,
the flash layout and the firmware, which is what a board profile is built from.
A report stays private unless you choose to publish it.

**Which cameras.** The ones we know of include the Amcrest IP4M-1026E, the
Intelbras VIP 3212 SD IR, the RVI IPC52Z12, the Speco O4P4X and a range of Dahua
models, listed by firmware version in
[firmware#2767](https://github.com/OpenIPC/firmware/issues/2767). At least one
Hikvision model, the DS-2CD2423G0-I, uses the S3Lm. The same firmware version
number turns up on Dahua cameras with other chips, so if you aren't sure what
your camera has, run ipctool and it will say.

**If you write code.** With an S3L camera and a serial console, you can help
bring up the next board. Join the firmware channel in [the chat](/community),
or start from [firmware#2767](https://github.com/OpenIPC/firmware/issues/2767).
