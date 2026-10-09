---
title: A camera with no speaker plays tunes, and shares itself by link
date: 2026-10-03
summary: "The IR-cut filter's coil becomes a buzzer that plays Nokia ringtones, Wi-Fi is set up by showing the camera a QR code, and a camera can be shown to anyone by a link."
author: OpenIPC team
---

What's new:

- **A camera can beep even with no speaker in it.** It beeps with the
  infrared filter: the coil that moves it clicks on every pulse, and a few
  hundred pulses a second turn the clicks into a hum whose pitch you choose.
  Exactly how the Commodore 1541 drive was made to sing in 1985 — it played
  *Daisy Bell* with its head. The firmware has ready-made cues, and you can
  send a tune of your own in RTTTL. That is the Nokia ringtone format: any of
  those thousands of tunes plays unchanged. Handy for hearing that a camera
  has booted or that the network is gone, and for working out which of five
  cameras on the roof is the one.
  <https://github.com/OpenIPC/wiki/blob/master/en/ircut-tunes.md>

- **Wi-Fi is set up with a QR code**: show the camera a code and it reads it
  itself. Firmware with the scanner looks for a code for half a minute after
  power-on. A code from [our generator](/tools/qr-code-generator) will do, and
  so will the one a phone shows when it shares its network; spaces and
  non-Latin letters in the password are no trouble. The camera first makes
  sure the network lets it in, and only then saves. It reports the result on
  that same filter: two short rising notes — the code was read; four notes up
  — connected, got an address, saving and rebooting; three low buzzes — wrong
  password. If it did not work, the camera keeps looking: fix the code on the
  phone and show it again.
  <https://github.com/OpenIPC/wiki/blob/master/en/wireless-settings.md#or-show-the-camera-a-qr-code>

- **A camera can be handed to your own AI assistant to identify.** The
  procedure for it is at [agents.md](/agents.md), and
  [the report page](/cameras/report) has a ready-made line: copy it to the
  assistant and it works on its own from there. First it runs ipctool and
  asks the catalogue whether it knows the board. It does — that is it, take
  the firmware. It does not — the assistant takes the boot log and the U-Boot
  environment, reads the flash with your permission, and sends a report. The
  rules are on the page too: the camera has to be yours, no guessing
  passwords, and the whole flash only with permission, because your Wi-Fi
  passwords are in it. Nothing goes anywhere until the project team has
  reviewed the report, and the MAC, the die number and the cloud id are
  replaced with hashes when it is published.
  <https://openipc.org/cameras/report>
  <https://openipc.org/agents.md>

- **The board catalogue.** Every board we have, sorted by maker: one board,
  one card, even if five sources wrote about it. Open a card and you see what
  each of them says — the maker's documentation, a seller's firmware, the
  Anjoy Vision archive, what owners sent in. Search goes by the text and not
  only the name, so a board is found by a chip marking or by a line out of a
  boot log. What you have open is what is in the address: send the link and
  the other person sees exactly that.
  <https://openipc.org/cameras/boards>

- **The OpenIPC Club.** Got a board? Tell us about it straight from its page:
  the boot log, the U-Boot console, ipctool's output, photos, a flash dump.
  Sign in and what you sent stays yours; you can sign in through the Telegram
  bot, through GitHub, or by a link to your email. Each accepted item earns a
  star, and a dump we did not have earns ten. Dumps are seen by you and the
  project team only.
  <https://openipc.org/club>

- **A camera can be shown by a link.** You make the link on the camera itself
  and send it to whoever you like: they open it in a browser and see the
  camera. No port to forward and no VPN to raise. The link's key sits in the
  address after the hash, and browsers do not send that part of an address to
  a server — so we do not have it, and the camera and the guest check each
  other themselves. The link opens not just the picture but the camera's whole
  interface. And if you gave view-only access, the guest gets a player with
  sound, a snapshot and full screen: WebRTC first, under a second of latency;
  if that will not go, MSE; MJPEG at worst.

Also worth a look:

- **The supported hardware page** opens with four ways to install OpenIPC,
  easiest first: buy a camera that already has it; switch your own over the
  air with a Coupler image; open it up and read the chip marking; ask the
  community. Then comes the chip table, and each one says whether it installs
  by the guide, is for the experienced only, or cannot be done yet.
  <https://openipc.org/supported-hardware>

- **Firmware is now looked up by chip**: pick the chip, then the variant, then
  the build from a calendar of the nights it was built on.
  <https://openipc.org/firmware-explorer>
