---
title: "OpenIPC 让被厂商停留在 3.0 的摄像机芯片跑上 Linux 7.0"
date: 2026-11-06
summary: "海思 Hi3516 系列摄像机芯片被留在 Linux 3.0.8 至 4.9，厂商再未提供更新的内核。OpenIPC 现在为其中七款芯片构建基于 Linux 7.0 的固件——在 Hi3516EV300 上已投入生产，在真实的 Hi3516CV300 硬件上已能启动。"
author: "OpenIPC 团队"
---

海思 Hi3516 系列摄像机芯片出厂时搭载 Linux 3.0.8 至 4.9，厂商再未为它们提供更新的内核。基于这些芯片的摄像机至今仍在网络中运行，跑着十年前就已冻结的内核。OpenIPC 现在为其中七款芯片构建基于 Linux 7.0 的固件。

这些构建被称为 neo，由 CI 从 OpenIPC 固件主仓库产出：Hi3516CV100、CV200、CV300、AV100、AV200、AV300 和 EV300。在 Hi3516EV300 上，7.0 的构建已投入生产。在 Hi3516CV300 上，它能在真实板卡上启动并输出视频。其余五款可以在我们的模拟器中构建并启动，硬件验证正在进行。

难点从来不是内核本身。厂商以预编译内核模块的形式提供视频通路，这些模块通过符号版本控制绑定到唯一一个内核版本，因此为 4.9 编译的模块在任何更新的内核上都会拒绝加载。我们保留这些二进制文件，并从源码重建它们周围的一切——操作系统抽象层、内存管理器、MIPI、看门狗、ISP 固件加载器——统一置于一个覆盖 Linux 3.0 到 7.0 的兼容头文件之后。同一份驱动源码现在可以为 4.9、6.6、6.18 和 7.0 编译，其中没有任何条件编译代码。正是这一点解开了摄像机视频通路与冻结内核之间的绑定。

迁移到当前内核是一项安全层面的改变：4.9 之后合入上游的修复现在能够到达这些设备。我们在改动接近摄像机之前就对其进行测试。我们的模拟器 [qemu-bench](https://github.com/OpenIPC/qemu-bench) 建模了来自十家厂商的一百多台监控设备，并在每次 push 时启动它们；而模拟器本身我们已与硅片比对过——真实 Hi3516EV300 板卡上硬件加速器的输出与模拟器逐像素一致，八项操作全部吻合。

内核位于 [OpenIPC/linux](https://github.com/OpenIPC/linux) 的 `upstream-patches` 分支；开源驱动在 [OpenIPC/openhisilicon](https://github.com/OpenIPC/openhisilicon)，固件在 [OpenIPC/firmware](https://github.com/OpenIPC/firmware)。安全问题请报告至 [OpenIPC/firmware/security](https://github.com/OpenIPC/firmware/security)。

镜像以每夜构建的形式发布在[发布页面](https://github.com/OpenIPC/firmware/releases)——neo 文件以 `-neo.tgz` 结尾。安装方法见[入门指南](/get-started)。
