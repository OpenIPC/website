package boards

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing/fstest"
)

// archive is a small docs/hardware tree in the OpenHisiIpCam layout, with
// one of each case the real one has: two units of one model, a heading that
// knows nothing (the console knows the SoC), the hi3618ev200 typo, a model
// name with a slash in it, files the page never linked, and a board
// directory the page never listed.
func archive() fstest.MapFS {
	md := `# List of known compatible HiSilicon based hardware

## hi3516cv100 family

##### XM / 53h20-s / hi3516cv100 / SONY IMX122/222/322
[![](/hardware/images/hi3516cv100/1/s/front.jpg)](/hardware/images/hi3516cv100/1/b/front.jpg)
[![](/hardware/images/hi3516cv100/1/s/pinouts1.jpg)](/hardware/images/hi3516cv100/1/b/pinouts1.jpg)

* [Dump](/hardware/dumps/hi3516cv100-1.bin)
* [U-boot settings](/hardware/dumps/hi3516cv100-1.uboot)

##### XM / 53h20-s / hi3516cv100 / SONY IMX222
[![](/hardware/images/hi3516cv100/2/s/back.jpg)](/hardware/images/hi3516cv100/2/b/back.jpg)

## hi3516cv200 family

##### ? / ? / ? / ?
[![](/hardware/images/hi3516cv200/2/s/front.jpg)](/hardware/images/hi3516cv200/2/b/front.jpg)

* [Dump](/hardware/dumps/hi3516cv200-2.bin)
* [U-boot settings](/hardware/dumps/hi3516cv200-2.uboot)

##### ? / ? / hi3618ev200 / ?
[![](/hardware/images/hi3516cv200/5/s/1.jpg)](/hardware/images/hi3516cv200/5/b/1.jpg)

## hi3516cv300 family

##### JVT / S290H16XF/S291H16XF / hi3516cv300 / SONY IMX290/291
[![](/hardware/images/hi3516cv300/1/s/front.jpg)](/hardware/images/hi3516cv300/1/b/front.jpg)
`
	console1 := "U-Boot 2010.06 (Aug 04 2016)\r\nSPI Nor(cs 0) ID: 0xc2 0x20 0x17\r\nBlock:64KB Chip:8MB Name:\"MX25L6406E\"\r\n" +
		"hisilicon # printenv\r\nbootcmd=setenv setargs setenv bootargs ${bootargs};run setargs;fload;bootm 0x82000000\r\n" +
		"bootargs=mem=${osmem} console=ttyAMA0,115200 root=/dev/mtdblock1 rootfstype=cramfs mtdparts=hi_sfc:256K(boot),3520K(romfs)\r\n" +
		"ethaddr=00:12:12:16:b0:c2\r\nloady   - load binary file over serial line (ymodem mode)\r\n"
	console2 := "hi3518ev200 System startup\nU-Boot 2010.06-svn2938\nBlock:64KB Chip:16MB Name:\"xm25qh128a\"\n" +
		"bootargs=console=ttyAMA0,115200 mtdparts=hi_sfc:256K(uboot),2368K(kernel)\nethaddr=00:00:00:00:00:00\n"
	f := fstest.MapFS{
		"known-compatible-hardware.md":           {Data: []byte(md)},
		"images/hi3516cv100/1/b/front.jpg":       {Data: jpg(800, 600)},
		"images/hi3516cv100/1/s/front.jpg":       {Data: jpg(150, 112)},
		"images/hi3516cv100/1/b/pinouts1.jpg":    {Data: jpg(600, 800)},
		"images/hi3516cv100/2/b/back.jpg":        {Data: jpg(300, 200)},
		"images/hi3516cv200/2/b/front.jpg":       {Data: jpg(640, 480)},
		"images/hi3516cv200/5/b/1.jpg":           {Data: jpg(640, 480)},
		"images/hi3516cv300/1/b/front.jpg":       {Data: jpg(640, 480)},
		"images/hi3516cv300/1/tmp/info":          {Data: []byte("192.168.1.88\nadmin\nadmin\nrtsp://192.168.1.88/av0_0\n")},
		"images/hi3516cv300/1/tmp/S290H16XF.pdf": {Data: []byte("%PDF-1.4 board manual")},
		"images/hi3516cv300/6/info":              {Data: []byte("HSELL\nHi3516ev100\nF22\nIP16EF22-2\n")},
		"images/hi3516cv300/6/b/pinouts1.jpg":    {Data: jpg(500, 500)},
		"dumps/hi3516cv100-1.bin":                {Data: bytes.Repeat([]byte{0xff}, 4096)},
		"dumps/hi3516cv100-1.uboot":              {Data: []byte(console1)},
		"dumps/hi3516cv200-2.bin":                {Data: bytes.Repeat([]byte{0x00}, 2048)},
		"dumps/hi3516cv200-2.uboot":              {Data: []byte(console2)},
	}
	return f
}

// supported is the catalogue for the fixture: what OpenIPC supports.
func supported(label string) string {
	switch label {
	case "hi3516cv100", "hi3518ev200", "hi3516cv300", "hi3516ev100":
		return label
	}
	return ""
}

func jpg(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 0x80, 0xff})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}
