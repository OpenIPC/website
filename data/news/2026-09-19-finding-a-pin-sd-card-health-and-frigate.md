---
title: Finding a pin, a card that lies, and Frigate turned around
date: 2026-09-19
summary: "The camera works out what is soldered to each pad, an SD card can keep nothing and say nothing, and Frigate can be run the other way round so nothing connects to the camera at all."
author: OpenIPC team
---

What turned up that is worth a look:

- **The camera works out for itself what is soldered to which pad.**
  Settings → Pins shows every pad the processor has and which of them are
  taken. The free ones can be checked one at a time: the camera drives a pad,
  you watch for what happens. That is how the IR-cut filter, the lamp and the
  button are found on a board with no diagram. HiSilicon, Goke, SigmaStar,
  Ingenic.
  <https://github.com/OpenIPC/wiki/blob/master/en/finding-a-gpio.md>

- **Two cameras on one scene.** One sees wide, the other looks closely at a
  corner of the same scene. Pair them and a dotted rectangle appears on the
  wide camera's picture: that is what the second one sees. Click it and its
  live picture goes inside. Zoom in and you are looking at the second
  camera's detail inside the first one's frame.
  <https://github.com/OpenIPC/wiki/blob/master/en/two-cameras-one-scene.md>

- **How many viewers a camera will take.** A few stalled connections could eat
  all the memory, after which the camera rebooted every couple of minutes with
  nothing left in the logs. There is a total ceiling now, and a table by board
  in the article: on 27 MB it comes to three or four viewers; on 121 MB it is
  no longer memory that limits you but sixteen connections.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#how-many-people-can-watch-at-once>

- **How to copy stock firmware, every way there is.** Take the copy before you
  write anything to the camera, and take the whole chip. The MAC address, the
  factory calibration and the original bootloader are only there: without a
  copy, a camera with a dead bootloader cannot be saved. Six routes in the
  article, from the simple one on a running camera to a programmer when it
  shows no sign of life.
  <https://github.com/OpenIPC/wiki/blob/master/en/backup-stock-firmware.md>

- **A memory card can record nothing and not say so.** SD has no
  self-diagnostics. A broken card confirms the write, returns no error, and
  then reads back as different data. A counterfeit pretends to be large and,
  when the real space runs out, writes over the old. The web interface has a
  card check now; the article says what the result means.
  <https://github.com/OpenIPC/wiki/blob/master/en/sd-card-diagnostics.md>

- **Why the camera puts out more bitrate than you set.** Usually `maxQp` is to
  blame: it limits how hard the camera may compress the picture, and when that
  limit runs out it goes over the bitrate instead. In the measurements only
  that setting was changed, and the stream went from 758 to 4237 kbit/s
  against a target of 1024. The camera now notices the overshoot itself and
  warns about it.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#when-the-camera-exceeds-the-bitrate-you-set>

- **Frigate can be run the other way round**: the camera pushes the stream and
  Frigate only receives it. No connections to the camera are left at all, and
  the memory it was reserving for them is freed. On weak boards that shows.
  <https://github.com/OpenIPC/wiki/blob/master/en/howto-frigate-integration.md#letting-the-camera-publish-instead-of-being-polled>

- **The cloud view carries sound**, from the 17 September builds. Note: if the
  microphone is on, the sound goes out by itself with nothing configured. If
  you do not want it, one line turns it off.
  <https://github.com/OpenIPC/wiki/blob/master/en/howto-self-hosted-cloud-camera.md>

- **Dehaze, sharpening and noise reduction became settings.** The defaults are
  the same as before, so the picture on an untouched camera does not change.
  The article has measurements, and one result is unexpected: turning
  sharpening off made the picture worse on one camera.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#dehaze-sharpening-and-noise-reduction>

- **Number plate recognition right in the browser.** The camera hands over a
  frame and your browser reads it. The most useful part is that it explains
  why a plate could not be read.
  <https://github.com/OpenIPC/wiki/blob/master/en/raw-editor.md#plates>

- **The night lamp washes out exactly what you wanted to see.** A lamp close
  to a face or a plate takes them to white, and the camera does not notice: it
  holds the average brightness of the frame, and the average is fine. Two new
  settings watch the brightest parts and hold the lamp back.
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#when-the-lamp-burns-out-what-you-were-trying-to-see>

- **How to tell a camera what it is made of.** Two ways. Settings on the
  camera itself work at once but are lost on a factory reset. A device profile
  in the firmware builder is applied on first boot to every camera of that
  model and survives one.
  <https://github.com/OpenIPC/wiki/blob/master/en/per-device-settings.md>
