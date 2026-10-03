package keyframe

import (
	"image"
	"image/color"
	"slices"
)

// LumaW and LumaH are the size of the greyscale copy a frame's brightness is
// measured on: small enough that a camera's timestamp overlay is a few
// percent of it, and the same for both decoders.
const LumaW, LumaH = 64, 36

// Luma is a frame's brightness percentiles, 0..255, measured on a LumaW x
// LumaH greyscale copy. The 5th and 95th rather than the darkest and the
// brightest pixel, so an on-screen clock on a flat grey frame does not make
// it look like a picture. The wall's mosaic uses them to leave out frames
// with nothing in them (internal/snapshots, Showcase).
type Luma struct{ P5, P50, P95 uint8 }

// lumaOf takes the percentiles of a greyscale copy, one byte per pixel.
func lumaOf(grey []byte) Luma {
	if len(grey) == 0 {
		return Luma{}
	}
	px := slices.Clone(grey)
	slices.Sort(px)
	n := len(px)
	return Luma{P5: px[n*5/100], P50: px[n/2], P95: px[n*95/100]}
}

// lumaOfImage box-averages a decoded picture down to LumaW x LumaH and takes
// its percentiles. A JPEG decodes to YCbCr, whose Y plane is read directly;
// anything else goes through color.GrayModel.
func lumaOfImage(img image.Image) Luma {
	b := img.Bounds()
	if b.Dx() < LumaW || b.Dy() < LumaH {
		return Luma{}
	}
	yc, isYCbCr := img.(*image.YCbCr)
	grey := make([]byte, 0, LumaW*LumaH)
	for cy := range LumaH {
		y0, y1 := b.Min.Y+cy*b.Dy()/LumaH, b.Min.Y+(cy+1)*b.Dy()/LumaH
		for cx := range LumaW {
			x0, x1 := b.Min.X+cx*b.Dx()/LumaW, b.Min.X+(cx+1)*b.Dx()/LumaW
			var sum, count int
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					if isYCbCr {
						sum += int(yc.Y[yc.YOffset(x, y)])
					} else {
						sum += int(color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y)
					}
					count++
				}
			}
			grey = append(grey, byte(sum/count))
		}
	}
	return lumaOf(grey)
}
