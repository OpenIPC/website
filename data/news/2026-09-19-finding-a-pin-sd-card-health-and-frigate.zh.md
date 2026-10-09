---
title: "找引脚、会撒谎的存储卡，以及反过来的 Frigate"
date: 2026-09-19
summary: "摄像机自己判断每个焊盘上焊的是什么，SD 卡可能什么都录不进去也一声不吭，Frigate 还可以反过来运行，让任何东西都不去连接摄像机。"
author: "OpenIPC 团队"
---

本期值得关注的内容：

- **摄像机自己判断哪个焊盘上焊的是什么。**
  设置 → 引脚会列出处理器的全部焊盘以及哪些已被占用。空闲的焊盘可以逐个测试：摄像机驱动一个焊盘，你观察发生什么。在没有电路图的板卡上，红外滤光片、补光灯和按键就是这样找出来的。HiSilicon、Goke、SigmaStar、Ingenic。
  <https://github.com/OpenIPC/wiki/blob/master/en/finding-a-gpio.md>

- **一个场景，两台摄像机。** 一台看全景，另一台盯着同一场景的一个角落。把它们配对后，全景画面上会出现一个虚线框：那就是第二台看到的范围。点击它，它的实时画面就会嵌入进来。放大后，你是在第一台的画面里看第二台的细节。
  <https://github.com/OpenIPC/wiki/blob/master/en/two-cameras-one-scene.md>

- **一台摄像机能带多少个观看者。** 以前几个卡住的连接就可能耗尽全部内存，之后摄像机每隔几分钟重启一次，日志里什么都没留下。现在有了总量上限，文章里还有按板卡的表格：27 MB 上限是三四个观看者；121 MB 上限制你的不再是内存，而是十六个连接。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#how-many-people-can-watch-at-once>

- **如何用一切办法复制原厂固件。** 在往摄像机写入任何东西之前先做备份，而且要备份整颗芯片。MAC 地址、出厂校准数据和原始 bootloader 只存在于那里：没有备份，bootloader 损坏的摄像机就救不回来了。文章里有六条路径，从在运行中的摄像机上最简单的一条，到毫无生命迹象时用编程器的一条。
  <https://github.com/OpenIPC/wiki/blob/master/en/backup-stock-firmware.md>

- **存储卡可能什么都录不进去，还不告诉你。** SD 卡没有自检。一张坏卡会确认写入、不返回错误，然后读回来的却是别的数据。一张假卡假装容量很大，真实空间用完后就直接覆盖旧数据。网页界面现在有了存储卡检测；文章解释了各项结果的含义。
  <https://github.com/OpenIPC/wiki/blob/master/en/sd-card-diagnostics.md>

- **为什么摄像机输出的码率比你设的高。** 通常是 `maxQp` 的锅：它限制摄像机压缩画面的力度，这个限制用尽后就会突破码率。测试中只改了这一项设置，码流就从 758 涨到了 4237 kbit/s，而目标是 1024。现在摄像机会自己发现超限并发出警告。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#when-the-camera-exceeds-the-bitrate-you-set>

- **Frigate 可以反过来运行**：由摄像机推送码流，Frigate 只负责接收。不再有任何连接指向摄像机，原本为这些连接预留的内存也被释放了。在性能弱的板卡上效果明显。
  <https://github.com/OpenIPC/wiki/blob/master/en/howto-frigate-integration.md#letting-the-camera-publish-instead-of-being-polled>

- **云端画面带声音了**，从 9 月 17 日的构建版本开始。注意：如果麦克风开着，声音会在没有任何配置的情况下自动发出去。不想要的话，一行配置就能关掉。
  <https://github.com/OpenIPC/wiki/blob/master/en/howto-self-hosted-cloud-camera.md>

- **去雾、锐化和降噪变成了可调设置。** 默认值与之前相同，所以未动过的摄像机画面不会变。文章里有实测数据，其中一个结果出人意料：在某台摄像机上关掉锐化反而让画面变差了。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#dehaze-sharpening-and-noise-reduction>

- **直接在浏览器里识别车牌。** 摄像机交出一帧，你的浏览器来读取。最有用的部分是它会解释为什么某个车牌读不出来。
  <https://github.com/OpenIPC/wiki/blob/master/en/raw-editor.md#plates>

- **夜间补光灯把你想看的东西正好冲白了。** 靠近人脸或车牌的补光灯会把它们打成一片白，而摄像机毫无察觉：它维持的是帧的平均亮度，而平均值没问题。两个新设置会盯住最亮的部分，把补光灯压住。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#when-the-lamp-burns-out-what-you-were-trying-to-see>

- **如何告诉一台摄像机它由什么组成。** 两种方式。摄像机本机上的设置立即生效，但恢复出厂后会丢失。固件构建器中的设备配置文件会在该型号每台摄像机首次启动时生效，并且能扛过一次恢复出厂。
  <https://github.com/OpenIPC/wiki/blob/master/en/per-device-settings.md>
