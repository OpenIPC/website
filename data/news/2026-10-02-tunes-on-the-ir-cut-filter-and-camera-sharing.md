---
title: A camera with no speaker plays tunes, and shares itself by link
date: 2026-10-02
summary: "The IR-cut filter's coil becomes a buzzer that plays Nokia ringtones, Wi-Fi is set up by showing the camera a QR code, and a share link lets someone watch without a port forward."
author: OpenIPC team
---

## A camera with no speaker can play tunes

Most cameras have no speaker. Nearly all of them have an IR-cut filter: a
small shutter moved by a coil. Pulse that coil and it clicks; pulse it a few
hundred times a second and it buzzes at a pitch you choose.

That is the trick the Commodore 1541 drive-music demo used in 1985 to play
*Daisy Bell* on a floppy drive's head, and majestic now uses it. There are
built-in cues, and `/night/chime?rtttl=...` plays anything written in RTTTL —
the Nokia ringtone format, so any of the thousands of old Nokia ringtones
plays as it is.

Useful for a sound when something finishes booting, when the network drops, or
simply to find out which camera on the roof is which.

## Wi-Fi by showing the camera a QR code

Firmware with the scanner looks for a code for about half a minute after boot.
It takes the code [OpenIPC's generator](/tools/qr-code-generator) makes and
the one a phone shows when it shares its network; spaces and non-Latin
characters in the name or password are fine.

The camera joins the network before it saves anything, and reports what
happened on that same filter:

| you hear | meaning |
| --- | --- |
| two quick rising notes | the code was read |
| a rising four-note run | joined, got an address, saving and rebooting |
| three low buzzes | the network refused the password |
| high-low, twice | no network by that name answered |
| three falling notes | no code seen; scanning has stopped |

After a failure it reconnects to whatever it had and keeps looking, so you can
fix the code on your phone and show it again.

## Showing a camera by link

A camera with a live share holds a connection out to a relay. Whoever opens
the link is told how to reach the camera, and from then on the video goes
straight between the browser and the camera. No port to forward and no VPN to
set up.

The link's key sits after the hash, and browsers never send that part of an
address to a server, so the relay does not have it: the camera and the page
prove themselves to each other, bound to both ends' connection fingerprints.
A service worker carries the camera's own pages through the same tunnel, so
the link hands over the camera rather than a view of it.

A guest with a view-only link gets a real player: WebRTC first for sub-second
latency, MSE where WebRTC will not go, and the camera's MJPEG as a floor, with
sound on request, a snapshot that downloads, and full screen.

## Put an AI coding agent to work on a camera

[agents.md](/agents.md) is a protocol page for an AI coding agent, and
[the report page](/cameras/report) has the line to paste. The agent gets
ipctool's output, asks the catalogue whether the board is already known and
stops there with its firmware if it is; otherwise it collects the boot log and
the U-Boot environment, reads the flash if the owner agrees, and posts the lot
as a report.

The rules are on the page: only a camera its owner can open, no password
guessing, and ask before reading the whole flash, because that holds their
Wi-Fi keys. Nothing is published before a maintainer has reviewed it, and the
MAC, the die ID and the cloud ID are hashed in anything that is.

## Supported hardware, written for owners

Owners said [the page](/supported-hardware) was too technical: it asked which
chip at which stage of development, and step one was running ipctool, which
needs a serial console or a way into the vendor's Linux.

It now opens with four ways in, easiest first. Buy a camera with OpenIPC
already on it; switch one over the air with a Coupler image; open the case and
read the chip; or ask the community. The chip table follows, with a column
that answers the question people actually have: guided install, experts only,
or not yet.

The [firmware explorer](/firmware-explorer) was turned around the same way. It
used to ask which CI built the image, which is nobody's question; you now pick
the chip, then the variant, then the build from a calendar of the nights that
built it.
