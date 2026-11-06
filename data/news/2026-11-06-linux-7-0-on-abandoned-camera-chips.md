---
title: OpenIPC runs Linux 7.0 on camera chips their vendor left on 3.0
date: 2026-11-06
summary: "HiSilicon's Hi3516 camera chips were left on Linux 3.0.8 to 4.9 and never got a newer kernel from the vendor. OpenIPC now builds firmware for seven of them on Linux 7.0 — in production on the Hi3516EV300, and booting on real Hi3516CV300 hardware."
author: OpenIPC team
---

HiSilicon's Hi3516 camera chips shipped with Linux 3.0.8 to 4.9, and the
vendor never gave them a newer kernel. Cameras built on them are still on
networks today, running kernels frozen a decade ago. OpenIPC now builds
firmware for seven of these chips on Linux 7.0.

The builds are called "neo" and come out of CI from the main OpenIPC firmware
tree: Hi3516CV100, CV200, CV300, AV100, AV200, AV300 and EV300. On the
Hi3516EV300 the 7.0 build is in production. On the Hi3516CV300 it boots on a
real board and serves video. The other five build and boot in our emulator,
and hardware verification is under way.

The kernel was never the hard part. The vendor's video pipeline arrives as
pre-compiled kernel modules tied to a single kernel version by symbol
versioning, so a module built for 4.9 refuses to load on anything newer. We
keep those binaries and rebuild everything around them from source — the OS
abstraction layer, the memory manager, MIPI, the watchdog, the ISP firmware
loader — behind one compatibility header that spans Linux 3.0 to 7.0. The same
driver source now compiles for 4.9, 6.6, 6.18 and 7.0 with no conditional code
in it. That is what breaks the tie between a camera's video pipeline and a
frozen kernel.

Moving to a current kernel is a security change: upstream fixes merged since
4.9 reach these devices now. We test changes before they get near a camera.
Our [qemu-bench](https://github.com/OpenIPC/qemu-bench) emulator models more
than a hundred surveillance machines from ten vendors and boots them on every
push, and we checked the emulator itself against silicon — the hardware
accelerator's output on a real Hi3516EV300 board matches it pixel for pixel,
across all eight operations.

The kernel is at [OpenIPC/linux](https://github.com/OpenIPC/linux), branch
`upstream-patches`; the open drivers are in
[OpenIPC/openhisilicon](https://github.com/OpenIPC/openhisilicon) and the
firmware in [OpenIPC/firmware](https://github.com/OpenIPC/firmware). Security
reports go to
[OpenIPC/firmware/security](https://github.com/OpenIPC/firmware/security).

Images are published as nightly builds on the
[releases page](https://github.com/OpenIPC/firmware/releases) — the neo files
end in `-neo.tgz`. Installation is covered in [getting started](/get-started).
