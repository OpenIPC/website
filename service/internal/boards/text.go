package boards

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
)

var envLine = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]{0,63})=(.*)$`)

// UBootVars reads the name=value lines of a U-Boot console capture:
// `printenv` output, among the banner and the help text around it. A name
// printed twice keeps its last value, as U-Boot itself would.
func UBootVars(console string) map[string]string {
	vars := map[string]string{}
	for _, line := range strings.Split(console, "\n") {
		if m := envLine.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			vars[m[1]] = m[2]
		}
	}
	return vars
}

// Thumbnail scales a photo down to fit max×max, averaging every source
// pixel into the one it lands on, and returns the JPEG with the original's
// size. The archive's own thumbnails are 150 pixels, too small for a card.
func Thumbnail(src []byte, max int) (thumb []byte, width, height int, err error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	width, height = b.Dx(), b.Dy()
	tw, th := width, height
	if tw > max || th > max {
		if tw >= th {
			tw, th = max, max*height/width
		} else {
			tw, th = max*width/height, max
		}
	}
	tw, th = maxInt(tw, 1), maxInt(th, 1)
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		y0, y1 := b.Min.Y+y*height/th, b.Min.Y+maxInt((y+1)*height/th, y*height/th+1)
		for x := 0; x < tw; x++ {
			x0, x1 := b.Min.X+x*width/tw, b.Min.X+maxInt((x+1)*width/tw, x*width/tw+1)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := img.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			out.Set(x, y, color.RGBA64{uint16(r / n), uint16(g / n), uint16(bl / n), 0xffff})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 80}); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), width, height, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
