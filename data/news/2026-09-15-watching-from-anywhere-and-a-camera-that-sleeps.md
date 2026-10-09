---
title: Watching from anywhere, and a camera that sleeps
date: 2026-09-15
summary: "Your own cloud view of a camera without a vendor, a camera that halves its power draw when nobody is watching, encrypted recordings, and swapping the SD card without stopping the stream."
author: OpenIPC team
---

What turned up that is worth a look:

- **Your own "cloud" view of a camera from anywhere** — no port forwarding, no
  dynamic DNS, no vendor server. The camera pushes its video over WHIP to your
  own VPS and you watch it in a browser; latency is a couple of hundred
  milliseconds instead of the several seconds HLS costs.
  <https://github.com/OpenIPC/wiki/blob/master/en/howto-self-hosted-cloud-camera.md>

- **The camera sleeps when nobody is watching**: the sensor and the image
  pipeline stop, not just the encoders. 2.06 W → 1.01 W on a Hi3516EV300 with
  an IMX335, while RTSP, the API and the microphone keep working.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#stopping-the-sensor-and-isp-when-nothing-is-watching>

- **HLS no longer conflicts with recording** — the two together are cheaper:
  the playlist points at the clip already being written to the card, nothing
  is held in RAM, and the live edge is about a second behind rather than a
  whole GOP.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#live-hls>

- **Encrypted recordings on the card**: four modes and an honest table of what
  each one really protects against — the card stolen, the flash cloned, the
  whole camera carried off. And why the old `records.key` was not encryption
  and is gone.
  <https://github.com/OpenIPC/wiki/blob/master/en/recording-encryption.md>

- **Detections are published in one format for everything**: a websocket, an
  HTTP endpoint, an overlay in the browser, ONVIF metadata, a track inside the
  recording, and a per-day index the timeline is drawn from.
  <https://github.com/OpenIPC/wiki/blob/master/en/analytics-metadata.md>

- **Changing the SD card on a running camera**: a wizard in the web interface
  closes the current clip, unmounts the card and waits for the next one — the
  stream is not interrupted. And an explanation of why, on firmware older than
  September, pulling the card silently killed the slot until a reboot.
  <https://github.com/OpenIPC/wiki/blob/master/en/sd-card-swap.md>

- **Raw**: a frame straight off the sensor as Adobe DNG, and an editor in the
  browser — develop it, measure the sensor, calibrate colour against a chart
  (HiSilicon and Goke).
  <https://github.com/OpenIPC/wiki/blob/master/en/raw-editor.md>
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#raw-sensor-data-as-adobe-dng>

- **A second camera**: a USB (UVC) webcam is published beside the built-in one
  as a source of its own, with its own streams. The Goke gk7205v200/v500 OTG
  builds have it first; other platforms follow as they are tested on hardware.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#a-second-camera>

- **Setting audio levels by ear, from the web interface**: play a test tone
  through the speaker, set the microphone level from what comes back, and
  listen to the result.
  <https://github.com/OpenIPC/wiki/blob/master/en/audio-soundcheck.md>

- **What to do when the camera has seen something**: two hooks — one when
  movement starts, one when a clip is finished — a worked example with a
  script and a way to reject false alarms by the size of what moved, and how
  to get a clip at all on a camera with no card, with
  `GET /video.mp4?duration=N`.
  <https://github.com/OpenIPC/wiki/blob/master/en/motion-events.md>
