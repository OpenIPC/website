---
title: Recording on motion, failsafe recovery, and a camera that sleeps
date: 2026-09-15
summary: "A fortnight of firmware documentation: watch a camera from anywhere without a vendor, recover a bricked Goke over the network, and cut a camera's power draw in half when nobody is watching."
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

## Failsafe mode on Goke

An interrupted update used to leave a camera rebooting forever, with no
picture, no web interface and no shell, recoverable only with a soldering
iron. On gk7205v200 and gk7205v300 the bootloader now counts failed boots, and
after the limit it brings the camera up with networking and SSH only. From
there you can look at what went wrong and put it right over the network or
from an SD card.

The counter lives in RAM, so a healthy boot adds no flash wear, and pulling
the power grants a fresh set of attempts.

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
- **Who may call the camera over SIP**: a registering camera trusts its
  registrar and refuses everyone else, and deployments without one name the
  addresses or ask for a password.
- **`isp.exposure` means three different things.** A limit in milliseconds on
  HiSilicon, Goke and SigmaStar; the exposure itself in microseconds on
  Ingenic, where setting it also stops the metering. Three orders of magnitude
  apart.
- **Setting audio levels by ear** from the browser, with a test tone and a
  microphone meter, since the camera has no level anywhere in its API.
