package reports

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxBackup bounds a full-flash backup. NOR is 8-32 MB; a 128 MB SPI NAND
// with its UBI volumes read out fits, and nothing larger is a camera.
const MaxBackup = 256 << 20

// maxBlocks is ipctool's MAX_MTDBLOCKS with room to spare.
const maxBlocks = 64

// Backup is what an ipctool backup file holds, as `ipctool backup <file>` and
// `ipctool upload --backup` write it (ipctool src/backup.c, save_file): the
// YAML and a NUL, then each MTD partition -- or each UBI volume of a UBI
// partition -- as a little-endian uint32 length and that many bytes.
type Backup struct {
	YAML   string
	Blocks []int64 // each partition's length, in order
}

// Size is the flash the backup holds.
func (b Backup) Size() int64 {
	var n int64
	for _, l := range b.Blocks {
		n += l
	}
	return n
}

// ReadBackup checks that r is one whole backup, to its last byte, and
// returns its YAML and partition lengths. It reads r once, discarding the
// partitions' contents: the caller has already stored and hashed them.
func ReadBackup(r io.Reader) (Backup, error) {
	var b Backup
	br := bufio.NewReaderSize(r, MaxYAML+1)
	head, err := br.ReadSlice(0)
	if errors.Is(err, bufio.ErrBufferFull) {
		return b, fmt.Errorf("the backup does not start with ipctool's YAML and a NUL within %d KB", MaxYAML>>10)
	}
	if err != nil {
		return b, errors.New("the backup does not start with ipctool's YAML and a NUL")
	}
	b.YAML = string(bytes.TrimSuffix(head, []byte{0}))
	var total int64 = int64(len(head))
	for {
		var n uint32
		if err := binary.Read(br, binary.LittleEndian, &n); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return b, fmt.Errorf("partition %d: the length is cut short", len(b.Blocks)+1)
		}
		if n == 0 {
			return b, fmt.Errorf("partition %d is empty", len(b.Blocks)+1)
		}
		if len(b.Blocks) == maxBlocks {
			return b, fmt.Errorf("the backup has more than %d partitions", maxBlocks)
		}
		got, err := io.CopyN(io.Discard, br, int64(n))
		if err != nil {
			return b, fmt.Errorf("partition %d: %d bytes of %d", len(b.Blocks)+1, got, n)
		}
		b.Blocks = append(b.Blocks, int64(n))
		total += 4 + int64(n)
		if total > MaxBackup {
			return b, fmt.Errorf("the backup is larger than %d MB", MaxBackup>>20)
		}
	}
	if len(b.Blocks) == 0 {
		return b, errors.New("the backup holds ipctool's YAML but no flash")
	}
	return b, nil
}
