package keyframe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// CheckTimeout bounds one decode. A variable so a test can shorten it.
var CheckTimeout = 60 * time.Second

// ErrCheckTimeout is a decode that did not finish in CheckTimeout. It says
// nothing about the frame -- the machine may simply have been busy -- so it is
// a reason to try again later, not to refuse.
var ErrCheckTimeout = errors.New("the decode check timed out")

// Check has ffmpeg decode the frame exactly as a browser will receive it --
// the parameter sets from the configuration record followed by the access
// unit -- and requires one picture out with no decode error. That picture is
// scaled to a LumaW x LumaH full-range greyscale copy on the way out, which is
// what the frame's brightness (Luma) is measured on.
//
// Parse proves the file is shaped like a keyframe; only a decoder proves the
// bitstream inside is one. The wall passes these bytes on untouched to
// visitors' hardware decoders, so one that will not decode here is not
// published. Nothing Check produces is kept but the measurement.
func Check(ctx context.Context, ffmpeg string, f *Frame) (Luma, error) {
	stream, err := AnnexB(f)
	if err != nil {
		return Luma{}, err
	}
	format := "h264"
	if f.HEVC {
		format = "hevc"
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-v", "error", "-xerror", "-err_detect", "explode",
		"-f", format, "-i", "pipe:0", "-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:%d:out_range=full,format=gray", LumaW, LumaH), "-f", "rawvideo", "pipe:1")
	cmd.Stdin = bytes.NewReader(stream)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if parent.Err() == nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Luma{}, ErrCheckTimeout
		}
		return Luma{}, fmt.Errorf("%s: %w: %s", ffmpeg, err, strings.TrimSpace(stderr.String()))
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return Luma{}, fmt.Errorf("decoder complained: %s", msg)
	}
	if stdout.Len() != LumaW*LumaH {
		return Luma{}, errors.New("no picture decoded")
	}
	return lumaOf(stdout.Bytes()), nil
}
