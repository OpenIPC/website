---
title: Watching from anywhere, and a camera that sleeps
date: 2026-09-15
summary: "Watch a camera from anywhere without a vendor, cut its power draw in half when nobody is looking, and get a clip out of one that has no memory card at all."
author: OpenIPC team
---

A fortnight of work on the wiki, most of it describing firmware that had
landed without anyone writing it down.

## Watch a camera from anywhere, without a vendor

Flashing OpenIPC removes the manufacturer's cloud, which is usually the point.
It also removes the thing that made the vendor's app convenient: the camera
dialling out, so nothing had to be opened on the router.

A camera can now push its video over WHIP to a server you control, and you
watch it in a browser. Latency is a few hundred milliseconds against the
several seconds an HLS setup costs. The guide uses MediaMTX on the cheapest
VPS; the video passes through untouched, so the relay does no transcoding.

## A camera that sleeps when nobody is watching

Turning both streams off stops the encoders, but the sensor and the image
pipeline keep running, and that is where most of the power goes. On HiSilicon
and Goke the camera can now stop those too. Measured at the PoE port on a
Hi3516EV300 with an IMX335:

| state | power | die temperature |
| --- | --- | --- |
| both streams and audio | 2.06 W | 62.1 °C |
| encoders off, sensor still running | 1.77 W | 56.2 °C |
| idle-suspended | 1.01 W | 49.5 °C |

Audio, RTSP, the web server and the API keep running throughout.

## HLS runs beside recording now

The two used to be mutually exclusive. Running both is now the better
configuration rather than merely a permitted one: when the camera is
recording, the playlist describes byte ranges of the clip on the card instead
of holding a second copy in RAM, so it costs no memory, and the live edge is
about a second behind rather than a whole keyframe interval.

## Encrypted recordings

`records.encryption` decides what a thief gets when the card is pulled. There
are four modes, and the page is a table of what each one survives: the card
copied, the flash cloned, the camera stolen with a shell on it, the camera
dead. Only one of them survives a stolen camera, and it is the one where the
camera cannot play its own recordings.

The old `records.key`, which XORed the payload with a repeating key kept in
plain text beside it, was obfuscation rather than encryption and has been
removed.

## What the camera detects, and what to do about it

Detections are now published once, in one shape, and everything reads the same
answer: a websocket, a polling endpoint, a browser overlay, an ONVIF metadata
stream, a track inside the recording, and a per-day index the recordings page
draws a timeline from.

A second page covers the practical half — the two moments you can hook, the
script that runs when a clip closes, and how to get a clip at all on a camera
with no card, by asking its own HTTP server for one.

## Also

- **Changing the SD card on a running camera**, with a guided swap that closes
  the current clip first. On firmware older than September, pulling a card
  cost you the slot until a reboot, with nothing in the log to explain it.
- **Raw frames as Adobe DNG**, and a raw editor that opens one in the browser:
  develop, measure the sensor, calibrate colour from a chart.
- **A USB webcam as a second camera**, on Goke OTG builds first.
- **Setting audio levels by ear** from the browser, with a test tone and a
  microphone meter, since the camera has no level anywhere in its API.
