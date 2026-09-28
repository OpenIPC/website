package keyframe

import "errors"

// FullRange reports whether an H.265 frame's sequence parameter set says its
// samples use the full 0-255 range rather than the 16-235 "video" range.
//
// WebCodecs reads this itself. The WebAssembly fallback in the browser does
// not: it converts YUV to RGB by hand, and a camera that encodes full range --
// the lab's hi3516av300 does in H.265 and not in H.264 -- comes out visibly
// dark and flat when converted as video range. So the socket says which, and
// this is where it is read: the SPS's VUI, video_full_range_flag. Absent VUI
// or absent signal type means video range, as the standard says.
//
// The walk is ITU-T H.265 7.3.2.2 up to the VUI's video signal type, which
// means parsing everything before it, reference picture sets and scaling lists
// included; E.2.1 for the VUI.
func FullRange(f *Frame) (bool, error) {
	if !f.HEVC {
		return false, errors.New("not H.265")
	}
	sets, err := ParameterSets(f)
	if err != nil {
		return false, err
	}
	for _, nal := range sets {
		if len(nal) > 2 && nal[0]>>1&0x3F == 33 {
			return spsFullRange(unescape(nal[2:]))
		}
	}
	return false, errors.New("no SPS")
}

func unescape(b []byte) []byte {
	out := make([]byte, 0, len(b))
	zeros := 0
	for _, c := range b {
		if zeros >= 2 && c == 3 {
			zeros = 0
			continue
		}
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

type bits struct {
	b   []byte
	pos int
	err error
}

func (r *bits) u(n int) uint32 {
	var v uint32
	for range n {
		if r.pos >= len(r.b)*8 {
			r.err = errShort
			return 0
		}
		v = v<<1 | uint32(r.b[r.pos/8]>>(7-r.pos%8)&1)
		r.pos++
	}
	return v
}

func (r *bits) flag() bool { return r.u(1) == 1 }

func (r *bits) ue() uint32 {
	zeros := 0
	for r.u(1) == 0 {
		if r.err != nil || zeros > 31 {
			r.err = errors.New("bad exp-Golomb code")
			return 0
		}
		zeros++
	}
	return 1<<zeros - 1 + r.u(zeros)
}

func (r *bits) se() int32 {
	v := r.ue()
	if v&1 == 1 {
		return int32(v+1) / 2
	}
	return -int32(v / 2)
}

func profileTierLevel(r *bits, maxSubLayersMinus1 uint32) {
	r.u(8 + 32 + 48 + 8) // general profile, compatibility, constraints, level
	sub := make([][2]bool, maxSubLayersMinus1)
	for i := range sub {
		sub[i] = [2]bool{r.flag(), r.flag()}
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			r.u(2)
		}
	}
	for _, s := range sub {
		if s[0] {
			r.u(88)
		}
		if s[1] {
			r.u(8)
		}
	}
}

func scalingListData(r *bits) {
	for size := 0; size < 4; size++ {
		step := 1
		if size == 3 {
			step = 3
		}
		for matrix := 0; matrix < 6; matrix += step {
			if !r.flag() {
				r.ue() // scaling_list_pred_matrix_id_delta
				continue
			}
			coefs := min(64, 1<<(4+size*2))
			if size > 1 {
				r.se() // scaling_list_dc_coef_minus8
			}
			for range coefs {
				r.se()
			}
		}
	}
}

// stRefPicSet parses one st_ref_pic_set and returns its picture count, which a
// later set predicting from it needs.
func stRefPicSet(r *bits, idx uint32, num uint32, counts []uint32) uint32 {
	if idx != 0 && r.flag() { // inter_ref_pic_set_prediction_flag
		delta := uint32(1)
		if idx == num {
			delta = r.ue() + 1
		}
		r.u(1) // delta_rps_sign
		r.ue() // abs_delta_rps_minus1
		if delta > idx {
			r.err = errors.New("bad reference picture set")
			return 0
		}
		ref := counts[idx-delta]
		var n uint32
		for j := uint32(0); j <= ref; j++ {
			used := r.flag()
			if used || r.flag() { // use_delta_flag when not used
				n++
			}
		}
		return n
	}
	neg, pos := r.ue(), r.ue()
	if neg > 16 || pos > 16 {
		r.err = errors.New("bad reference picture set")
		return 0
	}
	for range neg + pos {
		r.ue()
		r.u(1)
	}
	return neg + pos
}

func spsFullRange(b []byte) (bool, error) {
	r := &bits{b: b}
	r.u(4)
	maxSub := r.u(3)
	r.u(1)
	profileTierLevel(r, maxSub)
	r.ue() // sps_seq_parameter_set_id
	if r.ue() == 3 {
		r.u(1) // separate_colour_plane_flag
	}
	r.ue()
	r.ue() // width, height
	if r.flag() {
		r.ue()
		r.ue()
		r.ue()
		r.ue()
	}
	r.ue()
	r.ue() // bit depths
	log2MaxPocLsb := r.ue() + 4
	first := maxSub
	if r.flag() { // sps_sub_layer_ordering_info_present_flag
		first = 0
	}
	for i := first; i <= maxSub; i++ {
		r.ue()
		r.ue()
		r.ue()
	}
	for range 6 { // coding block, transform block and hierarchy sizes
		r.ue()
	}
	if r.flag() { // scaling_list_enabled_flag
		if r.flag() {
			scalingListData(r)
		}
	}
	r.u(2)        // amp, sample_adaptive_offset
	if r.flag() { // pcm_enabled_flag
		r.u(8)
		r.ue()
		r.ue()
		r.u(1)
	}
	num := r.ue()
	if num > 64 {
		return false, errors.New("bad reference picture set count")
	}
	counts := make([]uint32, num+1)
	for i := uint32(0); i < num && r.err == nil; i++ {
		counts[i] = stRefPicSet(r, i, num, counts)
	}
	if r.flag() { // long_term_ref_pics_present_flag
		n := r.ue()
		if n > 32 {
			return false, errors.New("bad long-term count")
		}
		for range n {
			r.u(int(log2MaxPocLsb))
			r.u(1)
		}
	}
	r.u(2)         // temporal MVP, strong intra smoothing
	if !r.flag() { // vui_parameters_present_flag
		return false, r.err
	}
	if r.flag() { // aspect_ratio_info_present_flag
		if r.u(8) == 255 {
			r.u(32)
		}
	}
	if r.flag() { // overscan_info_present_flag
		r.u(1)
	}
	if !r.flag() { // video_signal_type_present_flag
		return false, r.err
	}
	r.u(3) // video_format
	full := r.flag()
	return full, r.err
}
