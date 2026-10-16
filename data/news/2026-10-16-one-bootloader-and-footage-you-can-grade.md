---
title: One bootloader for every chip, and footage you can grade
date: 2026-10-16
summary: "Every U-Boot moved into one repository on top of mainline, a camera can now shoot log footage with per-frame metadata and timecode, and a Chrome HDR regression we found has been fixed by Google."
author: OpenIPC team
---

## Every bootloader in one repository

U-Boot used to sit in a tree of its own per SoC, so the same fix had to be
made in each of them. We have put them all in
[one repository](https://github.com/OpenIPC/u-boot): a branch per family, on
top of a fork of mainline U-Boot, so fixes and features from upstream reach us
instead of staying there. The HiSilicon and Goke branch currently covers 38
SoCs, each built in CI.

## A camera that shoots footage meant to be graded

A ready-made picture out of an ordinary camera cannot really be fixed later: a
blown-out sky does not come back, and it will not match footage from another
camera. So whoever edits the video wants a raw frame from the camera rather
than a pretty one, and makes it look right on the computer afterwards.

Turn on `isp.cinema` and the camera starts producing exactly that. It does two
things nobody else can do for it. Every code value in the picture maps back to
scene light through a published formula, the same one for every take — so the
editor knows what they are looking at. And the camera writes down how each
frame was made: exposure, gains, white balance and colour matrix per frame, a
sidecar per clip, SMPTE timecode and RAW reference stills.

Transforms into ACES come with it — the common language colour is passed in
between editing tools: an input transform, an OCIO colour space and a LUT
generator. Nothing is fitted per camera, so they hold for any sensor.
Hi3516EV200, EV300, CV500 and Goke V200, V300.

## HDR video, and a Chrome bug fixed along the way

One setting, `isp.hdr`, and TVs and browsers show the picture as HDR: a sunlit
wall, a lamp, a window come out brighter than ordinary white while shadows and
mid-tones stay where they were. Where the chip and sensor can do WDR, turn it
on alongside — that is what captures the extra range in the first place.

Building this turned up a bug in Google Chrome, and Google has already fixed
it. From version 145, Chrome on macOS stopped showing HDR video as HDR at
certain frame sizes. It hands the picture to the system's video layer and
compares the frame's rectangle exactly; after a normalisation into fractions
the number does not always come back bit for bit, the comparison fails, and
the video loses its HDR headroom and plays flat. The affected heights include
1936, 1944 and 1952 lines — so the ordinary 5-megapixel 2592x1944 frame of a
sensor like the IMX335 is one of them. We tracked down the cause and
[reported it to Chromium](https://issues.chromium.org/issues/572225701), and
their engineers fixed it. Until a fixed Chrome is widespread, majestic with
HDR on keeps its streams to sizes the browser gets right.

## Detection without a neural accelerator

Not every camera has an NPU, so we taught the person detector to run on the
camera's own CPU. The network is tiny: 66.7 thousand parameters, a 320x192
input, integer arithmetic. The camera does not run it over the whole frame —
it cuts out the area around the movement and hands it only that. We published
three ready-made operating points with the model and measured the threshold
for each: fewer false alarms, a middle, or catch more.
The models are in [cpu-models](https://github.com/OpenIPC/cpu-models).

## Turning the camera, and choosing what it follows

You aim a pan/tilt camera by its picture now, with no buttons. Spot something
at the edge of the frame, drag the image, and the camera follows your finger.
Double-tap a point and it centres there. The pad is a thumbstick: how far you
push it from the middle is the speed, so the separate speed slider is gone.
And when the picture is flipped or on its side the arrows turn with it, so
"up" means up on the screen rather than up on the camera.

You can also tell the camera what to follow: a person, a cat or dog, or a
vehicle. It used to go after anything that moved, which sent it off after a
swaying branch. The white lamp works off the same thing — it lights when the
camera has seen something rather than on any movement in the frame.

Day and night can switch by the sun or by the clock, not only by the light.
And motion detection stopped reacting to the camera's own lamp.

## Counting people

Draw lines and zones on the picture and the camera counts: how many crossed
each line each way, how many people are in a zone right now, today by the hour
and the last week or month by the day, and how long people stay. An hour the
camera did not count is drawn as not counted, never as zero.

## A catalogue of 402 image sensors

[/cameras/sensors](/cameras/sensors) puts them side by side: a grid of
resolution against what a sensor is for, filters, a sortable table and a
comparison. Each sensor has a page with its specifications, release date, twin
and successor, its neighbours on the chart, and the boards in the catalogue
that report it. A board's sensor now links to its page.

## Two new SoCs

The Hi3536DV100 is an NVR part, Cortex-A7: the kernel and its modules are
built from the SDK's source, and the closed MPP modules are relinked from the
vendor's `.ko` files.

The other is the Goke GK7206V100, their current generation under the XMedia
Lotus name: two Cortex-A7 cores, a RISC-V companion, and the vendor's SVP
block that runs the neural networks — whose userspace we rebuilt from source.
Six dies in the family, memory inside the package, kernel 5.10.130. The image
builds in CI, though no board has run it yet, and the bootloader for two of
the revisions is already in the new U-Boot repository.
Both are in [openhisilicon](https://github.com/OpenIPC/openhisilicon).

## Also

- **A large clean-up on Xiongmai.** The XM510 serves RTSP again, `/mjpeg`
  actually sends frames rather than just a header, snapshots work with the
  second stream off, and a restart neither loses video nor is reported as a
  crash. On the XM530 the main stream sizes itself to a sensor smaller than
  1080p. And you can take a firmware apart without a camera on the desk: the
  [emulator](https://github.com/OpenIPC/qemu-hisilicon) has an `xm510` machine
  that boots both our `xm510_lite` release and a whole flash dump through the
  camera's own bootloader. The XM530 and XM550 machines are written and
  waiting on review.
- **Lite and Ultimate swap without a soldering iron**: through `sysupgrade` on
  the 16 MB layout, through ipctool on the 8 MB one.
- **The [board catalogue](/cameras/boards)** gained Longse and Herospeed, the
  Novatek NT98533, NT98560 and NT98561, the Hi3536C/D/E and the Goke GK7206V1.
  Search looks everywhere by default, and the gallery swipes, zooms and closes
  with Back.
- **A [crash report](/crashes)** now carries the log of the boot that failed,
  so a reboot loop shows what it tripped over.
- **A [Wi-Fi QR code generator](/tools/qr-code-generator)**: the standard code,
  built live and validated on the page.
