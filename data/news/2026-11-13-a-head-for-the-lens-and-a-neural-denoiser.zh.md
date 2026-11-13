---
title: "给镜头配上云台，摄像机用神经网络降噪"
date: 2026-11-13
summary: "摄像机现在可以同时驱动变焦镜头和其下方的云台；一台 Hi3516CV610 用 NPU 降噪，代价我们已实测；抄表器能发现自身的误读；无线小区之间的切换现在可以同时跨三个小区进行。"
author: "OpenIPC 团队"
---

## 摄像机可以同时驱动变焦镜头和云台底座

摄像机现在可以同时运行变焦镜头组和它所在的云台底座，即使两者不在同一条线缆上。`ptz.head` 指定底座的协议——目前是 Pelco-D——`ptz.headPort` 说明在哪里找到它：镜头自身的串口、一个独立的 tty，或者——对于网络中串口网关后面的底座——一个带网关主机和端口的 `tcp://` 地址。Sony VISCA 变焦镜头组现在作为受支持的镜头，这样的镜头组会以 ISO 形式把自身增益回报给摄像机，因此画面设置知道镜头在做什么。

在 XiongMai N51813L 上试过：UART0 上接 VISCA 镜头，socat 网关后面接 Pelco-D 底座，两者都通过 WebUI 操控盘驱动。接线之前先读[新页面](https://github.com/OpenIPC/wiki/blob/master/en/ptz-head.md)——XiongMai 的 CN2 “485” 端口是 3.3 V TTL，不是 RS-485，页面上写了怎么处理。

## 摄像机用神经网络降噪，我们实测了代价

带 NPU 的 Hi3516CV610 现在可以在画面编码前用降噪网络处理它，通过 `isp.aibnr`。[新页面](https://github.com/OpenIPC/wiki/blob/master/en/majestic-ai-noise-reduction.md)说明了哪些芯片变体带 NPU、如何从 `socmodel` 判断、ultimate 固件需要哪些模型文件，以及如何开启。

页面还直说了你要付出的代价。在 IMX662 上以 1080p 实测：NPU 每帧需要约 33 ms，因此以 30 fps 运行的传感器实际输出 28 帧，NPU 占用率 94%，空闲内存从 76 MB 降到 44 MB。用 `isp.aibnrFps: 25` 限制后，NPU 降到 82%。每侧最外层 8 个像素被裁掉，因为降噪器在那里会留下淡淡的图案。五百万像素下 NPU 大约能处理每秒 10 帧。

页面没有声称的是：昏暗、细节丰富的场景能好多少。这一点尚未测量，页面也如实说明。

## 白光灯有了自己的控制

白光灯不再跟随红外引脚的动作。`nightMode.whiteLightPin` 把它放到一个普通 GPIO 引脚上，在 SigmaStar 上它通过内核自带的 PWM 驱动调光，其频率、上下限和曲线与红外补光灯分开设置。`colorToGray` 决定灯为白光时画面是否变灰。这些键在[流媒体器的页面](https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md)中。

WebUI 也跟上了：其他开关旁边多了一个白光开关，红外滤光片开关现在标明了哪个方向是开，灯的引脚出现在引脚图上，四个灯开关在手机上也放得下。

## 抄表器发现读错数字时能察觉

上周摄像机学会了读取电表闪烁的脉冲 LED。实验室的 Energomera 会把 1 和 2 读成 7，这正是毁掉一次读数的问题——所以摄像机现在会用闪烁来核对数字。超出脉冲所允许范围的超前读数无论重复多少次都会被忽略，在闪烁计数期间读数绝不回退，卡在某个数字上的字轮计为停滞，而脉冲继续把数字带下去。两个新指标分别上报这两种情况，[majestic-meters](https://github.com/OpenIPC/wiki/blob/master/en/majestic-meters.md) 中有说明。

## 你可以通过 XM 录像机对讲

接入 XM NVR 或 XMEye 的摄像机现在支持双向音频，操作员可以回应站在摄像机前的人。

## 切换成功了，同时跨三个小区

上周这一节说调度器还在编写中，跨小区切换是最后一步。这一步已经完成。整条链路在 PCIe 卡上已经闭环——单小区调度、可靠传输、准入控制、跨小区统一时钟、协同调度，以及现在的先建后断切换。

现在台架上有三个小区同时运行，使用三个信道，设备凭自身的测量在它们之间移动：它上报自己听到的内容，控制器选择最强的邻区，设备 99.9 到 100% 的情况下按时隙到达新小区，时钟保持连续。让这一切生效的关键是暂停间隙——一个计划好的“失聪”时刻，设备在其中监听自己未接入的小区。没有这些间隙，六次切换中有两次完全测不到任何东西。

也有新硬件：RTL8822BE、RTL8822CE 和 RTL8852BE PCIe 卡。

这仍然是台架，而且我们对此依然坦诚：剩下的里程碑是真实路径上的移动设备——衰落、多台设备，以及户外行走。[跟踪 issue](https://github.com/OpenIPC/devourer/issues/254) 是进展所在之处。

## 你的闪存转储不再带走你的 Wi-Fi 密码

[板卡目录](/cameras/boards)由人们寄来的转储构建。转储包含的不只是板卡的身份信息，所以网站现在会在存储报告前剥离 Wi-Fi 密码、Gwell P2P 密钥和云凭据，携带其所有者机密的转储原本会被公开，现在则保持私密。照片在内存预算内生成缩略图并解码，而不是照单全收手机发来的内容。

## 其他

本周有三十二个仓库有改动。其中 **devourer**、**majestic-webui** 和 **wiki** 已在上文介绍，以下是其余的仓库。

- **qemu-bench** 构建了 Hi3516CV610 孪生体：第五代编码器、VI 和 VPSS 上线，ISP 统计采用 V5 的布局以便流媒体器的曝光和白平衡收敛，芯片的 29 个加速器操作作为 NPU 任务。白平衡在较旧的孪生体中也能收敛了，可以在摄像机前放置场景并逐帧测量，Xiongmai V1 闪存镜像可以启动。
- **microsnander** 成长为闪存编程器：新增 143 种 NOR 芯片和 32 种 NAND 芯片，不在表中的芯片可通过其 SFDP 表识别，支持 16 MB 以上的 NOR，出厂坏块可保留或跳过，两次读取一致才保存转储，CH347 写入速度提升约 30%。
- **majestic** 还把 Fullhan 拉齐到其他平台的水平——快照、实时滑块、AE 上限、OSD、通过色彩矩阵调节色调和饱和度、手动白平衡——并为第一代 HiSilicon 芯片带来了裁剪和插件、T31 旋转，以及遵循厂商规则的慢速快门。
- **linux** 带来了 Anyka EV200 和 EV300 采集、Fullhan FH8626 ARC 协处理器、SigmaStar 分区 NOR 布局，以及 PWM 焊盘修复。
- **firmware** 从源码构建 ATBM Wi-Fi 驱动，为 SigmaStar 提供分区 NOR 镜像，为 hi3520dv200 新增 neo 变体，并对照原厂摄像机调校了一长串传感器。
- **u-boot** 把 SigmaStar 提升到公共层，每个芯片都有 CI，NOR 和 NAND 都覆盖，并在真实芯片上启动了 Allwinner V831/V833。
- **openhisilicon** 恢复了六个 hi3516cv6xx 传感器驱动，并对照 Sony 的数据手册进行了核对。
- **ipctool** 学会了只读 GPIO 转储（每个引脚的复用和电平）、三个 Novatek 芯片的管芯温度，以及 AR0230、IMX662 和 Anyka 芯片。
- **builder** 新增七块板卡，其中包括 Tapo C210 v2、Amcrest S3L 和 HeimVision PE3020-W。
- **majestic-af** 获得了 VISCA 执行器和云台端口；**motors** 获得了用于调试的位操作 Pelco-D 发送器。
- **website** 还修复了板卡筛选和固件下载池化；**divinus**、**viewer**、**decoder**、**PixelPilot_rk** 和 **hevc-wasm** 接收了播放器和叠加层修复；**vectis** 和 **camex** 得到控制台打磨；**waybeam** 在 CV610 上驱动了 IMX335 和 IMX662；**openxiongmai** 调校了 SC5332 和 SC5235 增益；**coupler**、**defib**、**openingenic**、**u-boot-ingenic**、**atbm-wifi**、**atbm_60xx**、**rda-wlan** 和 **sbc-groundstations** 承载了板卡、驱动和构建工作。
