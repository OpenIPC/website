---
title: Finding a pin, a card that lies, and letting the camera publish
date: 2026-09-19
summary: "The camera works out what is soldered to each pad, an SD card page that catches a card keeping nothing, and Frigate turned around so nothing connects to the camera at all."
author: OpenIPC team
---

## The camera finds out what is wired to each pad

A converted camera comes with no wiring diagram. The vendor firmware knew
where the IR-cut filter and the infrared lamp were, and that knowledge went
with it.

**Settings → Pins** draws every pad the chip has, says what each one is
already carrying, and drives the free ones one at a time until something
changes. A pad already doing a job is greyed out, because driving it takes the
job away and nothing announces it: the console goes quiet, the sensor loses
its clock, the network drops. The chip answers on HiSilicon, Goke, SigmaStar
and Ingenic; where it cannot, the page says so rather than showing an
all-clear it has not earned.

## Is the SD card actually storing your footage?

A camera can stream, record, answer ONVIF and report healthy counters while
the card underneath keeps nothing at all.

Three things make cards different from a disk. There is no SMART, so the
card's registers describe what it was sold as rather than its condition. A
card can fail without reporting an error: one card measured here acknowledged
every write, logged nothing, and returned three different checksums from three
reads of the same sector. And a counterfeit card is a small die presented as a
large one, folding addresses back onto the start, so it records perfectly
until it laps itself and the archive stops going back more than a few hours.

The SD card page now answers this in three lines, and the page explains what
each one means.

## Frigate, turned around

Everything in the Frigate guide has Frigate opening connections to the camera.
It can be reversed: the camera publishes into the go2rtc inside Frigate over
WHIP, and Frigate's detect and record roles read that stream.

On a small board that is worth something concrete. Read off `/metrics` on a
gk7205v200 with 27 MB of RAM:

| how Frigate gets the video | connections to the camera | reserved for them |
| --- | --- | --- |
| RTSP, pulled | 2 | 2.5 MB, about half the board's budget |
| WHIP, pushed | 0 | 0 |

## How many people can watch at once

A viewer whose connection is slower than the stream makes the camera hold what
it has not sent. There is now a ceiling on the total across all of them,
worked out from the board's free memory at startup.

It matters most on a small board: without it, enough stalled viewers exhaust
the memory, Linux kills the streamer, and the watchdog resets the board. From
outside that looks like a camera rebooting every minute or two with nothing in
any log you can collect over the network.

On 27 MB boards the allowance comes to three or four viewers; above about
120 MB memory stops being the constraint and a ceiling of sixteen connections
per protocol applies instead.

## When the camera exceeds the bitrate you set

Usually `maxQp` is the reason: it is the hardest the encoder may compress, and
when it runs out of room it exceeds the rate instead. On a Hi3516EV300 with an
IMX335, changing only that setting at a target of 1024 kbit/s:

| `maxQp` | produced |
| --- | --- |
| 42 | 758 kbit/s |
| 30 | 4237 kbit/s |

The camera now works this out for itself and says so, on the dashboard and
beside the settings that cause it.

## Also

- **Backing up stock firmware**, one page and six routes, from a shell on the
  running camera to an external programmer. Take the whole chip before writing
  anything: the MAC address, the per-device calibration and the vendor
  bootloader exist nowhere else.
- **Two cameras, one scene**: the wide camera draws where the narrow one
  looks, and on a click lays its live picture over that part of its own.
- **Dehaze, sharpening and noise reduction are settings now**, with every
  default exactly what the camera did before. `isp.sharpen: false` means "stop
  overriding the sensor's tuning", not "no sharpening".
- **Number plates read in the browser** from a raw frame, and — more useful —
  an explanation of what is stopping the ones it cannot read.
- **A night lamp can burn out what you were trying to see.** Auto-exposure
  holds the average, so a bright lamp close to a face or a plate washes it out
  while the average stays where it should.
- **`fw_setenv memsz` does nothing on SigmaStar**: the bootloader rewrites it
  on every boot before `bootcmd`. `/proc/cmdline` is the honest answer.
- **`isp.antiFlicker`** for the banding mains lighting leaves in the picture.
