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

// Check has ffmpeg decode the frame exactly as a browser will receive it --
// the parameter sets from the configuration record followed by the access
// unit -- and requires one picture out with no decode error.
//
// Parse proves the file is shaped like a keyframe; only a decoder proves the
// bitstream inside is one. The wall passes these bytes on untouched to
// visitors' hardware decoders, so one that will not decode here is not
// published. Nothing Check produces is kept.
func Check(ctx context.Context, ffmpeg string, f *Frame) error {
	stream, err := AnnexB(f)
	if err != nil {
		return err
	}
	format := "h264"
	if f.HEVC {
		format = "hevc"
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-hide_banner", "-v", "error", "-xerror", "-err_detect", "explode",
		"-f", format, "-i", "pipe:0", "-frames:v", "1", "-f", "framemd5", "pipe:1")
	cmd.Stdin = bytes.NewReader(stream)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", ffmpeg, err, strings.TrimSpace(stderr.String()))
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return fmt.Errorf("decoder complained: %s", msg)
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			return nil
		}
	}
	return errors.New("no picture decoded")
}
