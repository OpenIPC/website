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
//
// Hash is a difference hash of the same copy: 64 bits, each saying whether a
// cell of a 9x8 grid is darker than its right-hand neighbour. Two frames of
// one scene differ in a few bits as the light moves; a camera sending one
// picture over and over sends the same hash (internal/wallstars).
type Luma struct {
	P5, P50, P95 uint8
	Hash         uint64
}

// lumaOf takes the percentiles and the hash of a LumaW x LumaH greyscale
// copy, one byte per pixel.
func lumaOf(grey []byte) Luma {
	if len(grey) == 0 {
		return Luma{}
	}
	px := slices.Clone(grey)
	slices.Sort(px)
	n := len(px)
	return Luma{P5: px[n*5/100], P50: px[n/2], P95: px[n*95/100], Hash: dhash(grey)}
}

// dhash box-averages the copy to 9x8 and compares each cell with the next.
func dhash(grey []byte) uint64 {
	if len(grey) != LumaW*LumaH {
		return 0
	}
	const gw, gh = 9, 8
	var cells [gh][gw]int
	for gy := range gh {
		y0, y1 := gy*LumaH/gh, (gy+1)*LumaH/gh
		for gx := range gw {
			x0, x1 := gx*LumaW/gw, (gx+1)*LumaW/gw
			sum := 0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					sum += int(grey[y*LumaW+x])
				}
			}
			cells[gy][gx] = sum / ((y1 - y0) * (x1 - x0))
		}
	}
	var h uint64
	for gy := range gh {
		for gx := range gw - 1 {
			h <<= 1
			if cells[gy][gx] < cells[gy][gx+1] {
				h |= 1
			}
		}
	}
	return h
}

// lumaOfImage box-averages a decoded picture down to LumaW x LumaH and takes
// its percentiles. A picture smaller than that in either direction is sampled
// instead, each cell reading at least one source pixel. A JPEG decodes to
// YCbCr, whose Y plane is read directly; anything else goes through
// color.GrayModel.
func lumaOfImage(img image.Image) Luma {
	b := img.Bounds()
	if b.Empty() {
		return Luma{}
	}
	yc, isYCbCr := img.(*image.YCbCr)
	grey := make([]byte, 0, LumaW*LumaH)
	for cy := range LumaH {
		y0, y1 := b.Min.Y+cy*b.Dy()/LumaH, b.Min.Y+(cy+1)*b.Dy()/LumaH
		y1 = max(y1, y0+1)
		for cx := range LumaW {
			x0, x1 := b.Min.X+cx*b.Dx()/LumaW, b.Min.X+(cx+1)*b.Dx()/LumaW
			x1 = max(x1, x0+1)
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
