---
title: Cameras that see, and stay sharp while they turn
date: 2026-10-09
summary: "YOLOv8 runs on the NPU of the GK7205V5x0 with the models in flash and no setup, a pan/tilt camera follows a person and stays sharp through the move, and anti-flicker stops guessing and tests the light."
author: OpenIPC team
---

## Object detection on the camera's own silicon

The GK7205V500, V510 and V530 carry a neural accelerator, and YOLOv8 now runs
on it: person, bicycle, car, motorcycle, bus, truck, cat and dog. A 640x384
frame takes 46-50 ms on the small model and 102-112 ms on the large one. What
it finds goes where motion already goes — the ONVIF metadata track, the boxes
drawn over the picture in the browser, the track inside a recording, and
`/api/v1/analytics`.

The part that matters: **the models sit in the firmware itself and the
accelerator comes up on its own.** Nothing is downloaded, nothing is compiled,
nothing is configured. That has not been true of these cameras before. It is
off by default and comes on with one line.

The models live in [xmnpu-models](https://github.com/OpenIPC/xmnpu-models),
and only CI builds them, so every file traces to the commit and the run that
made it. Before anything is published the quantized model has to find what the
full-precision one finds; where they disagree the build fails and publishes
nothing. Weights, calibration images and the class list are pinned by hash.

The driver underneath was rewritten and opened. The firmware used to carry the
vendor SDK's closed `npu.o`; it now carries
[our own](https://github.com/OpenIPC/openhisilicon), serving the vendor
library the same device and the same ioctls. It also fixes what the blob did
not: dropped jobs are freed, no path returns holding a mutex, and a running
job is not freed from the interrupt handler. The clock went from 450 to
600 MHz.

## A pan/tilt camera follows a person

There is a Follow button on the browser's pad, and the box around whoever it
is following is drawn there too.

It works in steps: while the head is still and the picture has settled, the
detector hands over the people it sees; the camera picks one and stays with
them, recognising them by the box nearest the last one; the head turns by as
many steps as it takes to put them in the middle of the frame. Nothing
detected while the head is moving is acted on, because those frames are
smeared. Touch the pad yourself, or move the camera over ONVIF or DVRIP, and
following pauses.

## Sharp while you are turning it

The blur during a turn was taken for a shortage of bitrate. It is the
exposure: the shutter is open while the picture moves. Measured head on, four
times the bitrate buys 1.4x the sharpness and a short exposure buys 3.4x.

The camera now knows when the motor is running and shortens the exposure only
for that time, so the picture is four times sharper through a turn and the
stream at rest does not grow by a bit. Holding a short exposure all the time
doubles a still camera's stream, which is why nobody does it that way.

Found along the way: why video tore for every viewer at once during a turn.
The stepper driver timed the gaps between steps in a busy loop, because the
kernel could only wait in units of 10 ms, and a turn was eating 70-86% of the
camera's CPU. After the kernel fix it is under 5%, and the motors got faster
as well — tilt went three times quicker, 13 to 41 steps a second. A viewer who
falls behind is now sent a keyframe as soon as the queue clears: a 0.1-0.3 s
pause instead of 0.8-1.0, and 2-3% of frames lost instead of 11-13%.

Measured on a Goke GK7205V510 with a MIS2009 sensor and a stepper head, 1080p.

## Anti-flicker stops guessing and tests the light

Mains lamps flicker a hundred times a second, and unless the shutter is a
whole number of half-periods the picture carries rolling bands. There was a
mode against them, but it only engaged at dusk and never in a brightly lit
room — which is exactly where the bands show. The shutter is now held to whole
periods at all times, and banding fell from about a fifth of the picture to
under one percent.

That broke daylight: in sun the camera wants a shutter of a couple of
milliseconds and may no longer go below ten, so the frame blows out. Telling
sun from lamp by brightness does not work — a room under a 200 W floodlight
reads 2.0x its target and an outdoor scene 2.46x, so any threshold gets one of
them wrong.

So the camera runs an experiment instead. When the picture stays well over
target at the shortest whole-period shutter, it shortens the shutter for a
moment and looks for the lamp's bands in what it sees. Bands there, the light
is mains and the mode stays on, and that scene is not asked again for an hour.
No bands, the light is steady, so it is the sun, and the shutter is let go.
The picture dips once while it checks, for under half a second.

## Storage saver, next to Xiongmai's

`storageSaver` is for cameras whose footage is kept for weeks, where the disk
costs more than the picture — the same job other makers call H.264+, H.265+ or
H.265X. Three levels: the first drops no frames at all, asks for about a
quarter less room, and is better than the defaults on the moving parts of a
picture; the two above save more, over half at the top, but start skipping
frames.

Xiongmai's mode was taken apart and reproduced on our bench, with every
setting read off their firmware while it ran, so the two are measured like for
like. On a quiet scene they save 13% and we save 27%. On a busy scene theirs
saves nothing at all: the stream grows 22% over the defaults, so it does worse
exactly when something is happening. On the moving parts of the picture, at
the same bitrate, ours draws them better than the defaults and loses not a
single frame, while theirs draws them worse and throws half the frames away.

## Also

- **A camera can read its SMS.** The operator's messages about activation, a
  top-up or the money running out, on a page instead of AT commands and hand-
  decoded UCS2. Balance by short code, sending your own, and forwarding
  incoming messages to Telegram.
- **A camera reports its own crashes.** The kernel's log survives the reboot
  and majestic leaves a dump with the last lines it wrote; either can be sent
  in, by hand or automatically. They are filed by a signature of the top five
  stack frames, so one bug across a hundred cameras is one row. The public
  list is at [/crashes](/crashes), and a crash you send earns Club stars.
- **The bitrate need not be set at all.** Zero now means "work it out", from
  the resolution, frame rate, codec and rate-control mode. That is the new
  default.
- **Night can stay in colour**, with the infrared and white lamps on separate
  channels.
- **A change of uplink no longer breaks anything.** Move a camera from wired
  to LTE or back and SIP, outgoing streams, WHIP and WebRTC sessions reconnect
  on the new address. An open share link survives it too.
