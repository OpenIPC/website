---
title: "随处观看，以及一台会休眠的摄像机"
date: 2026-09-15
summary: "无需厂商即可从任何地方查看自己的摄像机云端画面、无人观看时功耗减半的摄像机、加密录像，以及不中断码流更换 SD 卡。"
author: "OpenIPC 团队"
---

本期值得关注的内容：

- **从任何地方查看自己的“云”端摄像机画面** — 无需端口转发，无需动态 DNS，无需厂商服务器。摄像机通过 WHIP 将视频推送到你自己的 VPS，你在浏览器中观看；延迟只有几百毫秒，而不是 HLS 那样的数秒。
  <https://github.com/OpenIPC/wiki/blob/master/en/howto-self-hosted-cloud-camera.md>

- **无人观看时摄像机休眠**：传感器和图像流水线停止工作，而不只是编码器。在搭载 IMX335 的 Hi3516EV300 上从 2.06 W 降至 1.01 W，同时 RTSP、API 和麦克风继续工作。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#stopping-the-sensor-and-isp-when-nothing-is-watching>

- **HLS 不再与录像冲突** — 两者同时开启反而开销更低：播放列表直接指向正在写入卡中的片段，不占用内存，直播边缘只落后约一秒，而不是整整一个 GOP。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#live-hls>

- **卡上的加密录像**：四种模式，以及一张诚实的表格，说明每种模式真正能防御什么 — 卡被偷走、闪存被克隆、整台摄像机被搬走。还有为什么旧的 `records.key` 不算加密、已被移除。
  <https://github.com/OpenIPC/wiki/blob/master/en/recording-encryption.md>

- **检测以统一格式发布**：websocket、HTTP 端点、浏览器中的叠加显示、ONVIF 元数据、录像内的轨迹，以及绘制时间轴所用的按天索引。
  <https://github.com/OpenIPC/wiki/blob/master/en/analytics-metadata.md>

- **在运行中的摄像机上更换 SD 卡**：网页界面中的向导会关闭当前片段、卸载卡并等待下一张卡 — 码流不会中断。还解释了为什么在九月之前的固件上，拔卡会悄悄导致卡槽失效，直到重启。
  <https://github.com/OpenIPC/wiki/blob/master/en/sd-card-swap.md>

- **Raw**：直接从传感器获取一帧并输出为 Adobe DNG，以及浏览器中的编辑器 — 显影、测量传感器、对照色卡校准色彩（HiSilicon 和 Goke）。
  <https://github.com/OpenIPC/wiki/blob/master/en/raw-editor.md>
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#raw-sensor-data-as-adobe-dng>

- **第二台摄像机**：USB（UVC）摄像头与内置摄像头并列发布为独立源，拥有自己的码流。Goke gk7205v200/v500 OTG 构建版本率先支持；其他平台在硬件上测试通过后跟进。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#a-second-camera>

- **在网页界面中凭耳朵设置音频电平**：通过扬声器播放测试音，根据回传的声音设置麦克风电平，然后试听结果。
  <https://github.com/OpenIPC/wiki/blob/master/en/audio-soundcheck.md>

- **摄像机检测到目标后该怎么做**：两个钩子 — 一个在移动开始时触发，一个在片段完成时触发 — 附带脚本实例和按移动目标大小过滤误报的方法，以及在没有卡的摄像机上如何通过 `GET /video.mp4?duration=N` 获取片段。
  <https://github.com/OpenIPC/wiki/blob/master/en/motion-events.md>
