---
title: "靠耳朵对焦，以秒计的曝光"
date: 2026-09-25
summary: "支持 GK7201V200，电动镜头可以站在梯子上听摄像机声音来设置，另有一个页面介绍望远镜等极暗场景下以秒计的曝光。"
author: "OpenIPC 团队"
---

本周更新内容：

- **支持 GK7201V200** —— XiongMai 板卡上最便宜的 600 MHz ARM 芯片。

- **新增电动镜头页面。** 摄像机自动对焦，镜头可以在网页界面里驱动，但有意思的部分是靠耳朵对焦。站在梯子上的安装人员没时间看手机，所以摄像机会发出蜂鸣：画面越清晰，蜂鸣越快越高。转过了最佳点，会听到一个低音。转回来，蜂鸣变成稳定的长音，意思是停止旋转。你可以只听画面的一部分而不是全部：灯下的一块车牌，院子对面的一个门口。
  <https://github.com/OpenIPC/wiki/blob/master/en/autofocus.md>
  <https://github.com/OpenIPC/wiki/blob/master/en/autofocus.md#focusing-by-ear>

- **新增近乎全黑环境拍摄页面**：望远镜、X 光屏、夜空。一切都基于一条规则：曝光不能长于帧本身。没有单独的长曝光模式，只有慢速的摄像机。想要五秒曝光 —— 摄像机就得每五秒出一帧。

  由此而来的是主要的坑，能让人耗掉一整晚。传感器的速度由码流帧率决定，而不是由传感器配置决定，而且两条码流都得放慢。第二条码流留在出厂的十五帧，曝光就会停在 66 毫秒，无论你设多高，而摄像机看起来像坏了。码流不能低于每秒一帧：再往下就有一部分要进传感器配置。IMX335 在 5 MP 下极限约为 7.7 秒。

  发热也测了，几乎无关紧要：在室温下热噪声淹没在普通噪声里。真正增长的是坏点数量，就是自己发光的那些点。即便如此，一刻钟内也到不了帧的百分之一，而且很容易去除：坏点位置永远不变，盖上镜头拍一帧就能把它减掉。
  <https://github.com/OpenIPC/wiki/blob/master/en/very-long-exposure.md>

- **摄像机根据天气调整画面。** 它是在安装当天按当天的天气设置的，之后起了雾，或者太阳落得很低，画面里同时出现明亮的天空和深深的阴影。现在摄像机会监视自己输出的画面，最多调整四个参数。自九月构建版本起默认开启。它工作得很安静：画面没问题，它什么都不动。
  <https://github.com/OpenIPC/wiki/blob/master/en/automatic-image-tuning.md>

- **用厂商工具调画面，从头到尾写全了**：如何导出配置，放在哪里才能在重启后保留，以及如何烧进固件。还解释了一个老怪事：你设了一个值，它又变回去了。那是摄像机在和你同时调整同样的参数。现在可以让它在你工作时不来捣乱。它退避期间不保存任何东西，所以结果要自己保存。
  <https://github.com/OpenIPC/wiki/blob/master/en/image-quality-tuning.md>

- **红外滤光片和补光灯现在各有自己的模式**：自动、手动，或者不管它。“不管它”不等于“没接线”：引脚配置保持不变，变的只是由谁决定。排查原因时值得记住：滤光片在白天开着会让画面偏粉，问题可能出在模式而不是接线。切换时还新增了一个暂停，给那些看到彩色帧闪过的人。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#who-moves-the-filter-and-the-lamp>

- **摄像机统计自己往卡里写了多少**，并显示在 SD 卡页面上。计数器存在卡本身上，跟着卡一起换到另一台摄像机。数字大本身不代表有问题；那是里程。
  <https://github.com/OpenIPC/wiki/blob/master/en/sd-card-diagnostics.md#written-by-this-camera>

- **直接从传感器取原始帧**，不做任何处理，现在 SigmaStar 以及 Ingenic T31 和 T23 也能提供。以前只有 HiSilicon 和 Goke。SigmaStar 和 T23 上默认关闭；相关页面说明了原因和开启方法。
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#on-sigmastar>
  <https://github.com/OpenIPC/wiki/blob/master/en/majestic-streamer.md#on-ingenic-t31-and-t23>

- **小改动**：raw 编辑器的截图换了 —— 现在来自同一场景并带真实色卡，标签页也改用新名称。补充了更多 SSC377D 的细节。目录里的链接做了整理。
  <https://github.com/OpenIPC/wiki/blob/master/en/raw-editor.md>
  <https://github.com/OpenIPC/wiki/blob/master/en/gpio-settings.md>
