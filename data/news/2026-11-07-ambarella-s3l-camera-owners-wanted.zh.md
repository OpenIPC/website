---
title: "Ambarella S3L 即将加入 OpenIPC，我们需要这些摄像机的用户"
date: 2026-11-07
summary: "OpenIPC 固件现在可以为 Ambarella S3L 构建，并在一台摄像机上启动并输出视频。在镜像可以安全刷写之前，我们需要知道市面上有哪些 S3L 摄像机，而在原厂固件上运行一次 ipctool 并提交报告就能告诉我们。"
author: "OpenIPC 团队"
---

Ambarella S3L 是一款摄像机芯片，大华在它上面推出了一整个系列的网络摄像机，这些摄像机也以许多其他品牌出售。直到本周之前，OpenIPC 还没有能在它上面出视频的固件。现在情况变了，我们需要拥有这些摄像机的人。

## 已经可以工作的部分

- **内核。** [OpenIPC/linux](https://github.com/OpenIPC/linux) 中 `ambarella-s3l` 分支的 S3L 内核现在支持 overlayfs，因此设置在重启后得以保留。
- **视频。** Ambarella 的 DSP 微码来自厂商的 SDK，与我们自己编写的加载程序一起提供。我们的推流程序 Majestic 已包含在镜像中。
- **CI。** [OpenIPC/firmware](https://github.com/OpenIPC/firmware) 在每次修改时都会构建 `s3l_ultimate` 板型。如果内核会破坏厂商预编译的视频驱动，构建就会停止。
- **一台真实的摄像机。** 在一台使用 OV4689 传感器的 Amcrest IP4M-1026E 上，一位贡献者用该板子自己的配置文件构建的这套固件在上电后启动：DSP 启动，摄像机输出 2688×1512 的 H.264，设置在重启后保留。那次运行中的推流程序是这位贡献者自己的，而不是 Majestic。

## 尚未完成的部分

**请不要刷写夜间构建中的 S3L 镜像。** 它是通用镜像：不包含任何具体板子的设备树，而我们测试过的唯一一台摄像机需要它自己的设备树。从摄像机闪存启动已发布的镜像还没有人尝试过。让镜像对某台摄像机安全的是板子配置文件，而我们只能为已知的摄像机编写它。

## 我们需要你做的

如果你有一台使用 S3L 的摄像机，请给我们发一份报告。这只需要几分钟，不会改变摄像机上的任何东西，并且可以在原厂固件上完成：

1. 通过 telnet 或串口控制台获得摄像机上的 shell。
2. 按照[报告页面](/cameras/report)的说明操作。页面会说明如何在没有 curl 的原厂固件上下载并运行 ipctool，以及如何发送它的输出。

ipctool 能识别 Ambarella 芯片。报告会告诉我们芯片、传感器、闪存布局和固件，而板子配置文件正是由这些信息构建的。除非你自己选择公开，报告都是保密的。

**哪些摄像机。** 我们已知的包括 Amcrest IP4M-1026E、Intelbras VIP 3212 SD IR、RVI IPC52Z12、Speco O4P4X，以及一系列大华型号，按固件版本列在 [firmware#2767](https://github.com/OpenIPC/firmware/issues/2767) 中。至少有一款海康威视型号 DS-2CD2423G0-I 使用 S3Lm。同样的固件版本号也出现在使用其他芯片的大华摄像机上，所以如果你不确定自己的摄像机用的是什么芯片，运行 ipctool，它会告诉你。

**如果你会写代码。** 有一台 S3L 摄像机和串口控制台，你就可以帮助适配下一块板子。欢迎加入[聊天](/community)中的固件频道，或从 [firmware#2767](https://github.com/OpenIPC/firmware/issues/2767) 开始。
