package wizard

import (
	"fmt"
	"strings"
)

// doNotPaste stands for the helper's `do_not_copy_paste` span: a line that
// starts with markup, so it never reaches the exported lines and only sets a
// block's no_paste flag. Its text is irrelevant to the export; what matters is
// that it contains none of the caveat triggers below, which the English one
// ("# Enter commands line by line! ...") does not.
const doNotPaste = `<span class="text-danger"># Enter commands line by line! Do not copy and paste multiple lines at once!</span>`

// caveats is InstallationHelper::CAVEATS: trigger, then the note it earns,
// in the order the notes come out.
var caveats = [][2]string{
	{"printenv ethaddr", "mac_record_caveat_html"},
	{"sf lock", "lock_caveat_html"},
	{"&&", "compound_caveat_html"},
}

func caveatsFor(lines []string) []string {
	notes := []string{}
	for _, c := range caveats {
		for _, l := range lines {
			if strings.Contains(l, c[0]) {
				notes = append(notes, c[1])
				break
			}
		}
	}
	return notes
}

// blocks is WizardExport::BLOCKS, in order.
var blocks = []string{
	"firmware_backup", "flashing_everything", "flashing_uboot", "flashing_linux",
	"preparing_environment", "post_flash_environment", "restore_from_backup",
}

func (c *camera) lines(block string) []string {
	switch block {
	case "firmware_backup":
		return c.firmwareBackup()
	case "flashing_everything":
		return c.flashingEverything()
	case "flashing_uboot":
		return c.flashingUboot()
	case "flashing_linux":
		return c.flashingLinux()
	case "preparing_environment":
		return append([]string{doNotPaste}, c.layoutCommands()...)
	case "post_flash_environment":
		return append([]string{doNotPaste}, c.postFlashCommands()...)
	case "restore_from_backup":
		return c.restoreFromBackup()
	}
	panic("unknown block " + block)
}

func (c *camera) sdWifi() bool { return c.sd == "sd" && c.iface == "wifi" }

func (c *camera) env() string {
	return "setenv ipaddr " + c.ip + "; setenv serverip " + c.server
}

func (c *camera) unlock(text []string) []string {
	if c.flashType != "nand" {
		text = append(text, "sf probe 0; sf lock 0;")
	}
	return text
}

// guardedFlash is the one line that erases only after the transfer worked.
func (c *camera) guardedFlash(transfer, offset, eraseSize, writeSize string) string {
	return c.guardedWrite(transfer, offset, eraseSize, "write", writeSize)
}

// guardedWrite is guardedFlash with the write command named: `write.trimffs`
// for anything carrying a UBI image (see writeCmd).
func (c *camera) guardedWrite(transfer, offset, eraseSize, write, writeSize string) string {
	cmd := "sf"
	if c.flashType == "nand" {
		cmd = "nand"
	}
	return transfer + " && " + cmd + " erase " + offset + " " + eraseSize + " && " + cmd +
		" " + write + " " + c.soc.LoadAddress + " " + offset + " " + writeSize
}

// ubiFlash is guardedWrite for the UBI-only layout, where nothing on the
// flash has a size the wizard can know: the chip may be 128 MiB or bigger.
// The UBI partition is erased by name (`nand erase.part ubi`, which the
// u-boot-xmedia NAND build carries and the U-Boot step installs first), so
// to the end of the chip whatever its size; blocks left with stale data past
// the image would be corrupted PEBs to UBI. A full image, written from
// whatever U-Boot the camera has, erases the whole chip instead.
func (c *camera) ubiFlash(transfer string, whole bool) string {
	// The partition by name for the write too, so it lands where the erase
	// was: both resolve from the bootloader's mtdparts.
	erase, offset := "nand erase.part ubi", "ubi"
	if whole {
		erase, offset = "nand erase.chip", "0x0"
	}
	return transfer + " && " + erase + " && nand " + c.writeCmd() + " " + c.soc.LoadAddress + " " +
		offset + " ${filesize}"
}

func (c *camera) writeSizeFor(fixed string) string {
	if c.flashType == "nand" {
		return fixed
	}
	return "${filesize}"
}

func (c *camera) firmwareBackup() []string {
	if c.nand() {
		return c.nandBackup()
	}
	la := c.soc.LoadAddress
	text := []string{doNotPaste, "printenv ethaddr"}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff "+c.flashSizeHex())
	text = append(text, "sf probe 0; sf read "+la+" 0x0 "+c.flashSizeHex())
	if c.sdWifi() {
		text = append(text,
			"mmc dev 0; mmc erase 0x10 "+c.flashSizeBlocks()+"; mmc write "+la+" 0x10 "+c.flashSizeBlocks(),
			"",
			"# Use the following command to restore the backup to a file on a PC",
			"# (replace /dev/sdc with your SD card device):",
			"# sudo dd bs=512 skip=16 count="+c.flashSizeSectors()+" if=/dev/sdc of=./"+c.backupFilename())
	} else {
		text = append(text,
			"tftpput "+la+" "+c.flashSizeHex()+" "+c.backupFilename(),
			"# if there is no tftpput but tftp then run this instead",
			"# (the third argument is what makes tftp upload rather than download)",
			"tftp "+la+" "+c.backupFilename()+" "+c.flashSizeHex())
	}
	return text
}

// A NAND chip is backed up in nandChunks pieces of nandChunkHex: 128 MiB is
// more than the RAM U-Boot has to hold it in. `nand read` and `nand write`
// skip bad blocks, so a piece holds the first good blocks from its offset
// and runs past its end by as many blocks as it skipped. The pieces are kept
// as separate files and go back piece by piece, at the same offsets, onto
// the chip they came from, which has the same bad blocks.
const (
	nandChunkHex    = "0x800000"
	nandChunkBlocks = 0x4000 // nandChunkHex in 512-byte SD card blocks
	nandChunkSize   = 0x800000
	nandChunks      = 16 // nandSizeHex / nandChunkHex
)

func nandChunkOffset(i int) string { return fmt.Sprintf("0x%x", i*nandChunkSize) }

func nandChunkBlock(i int) string { return fmt.Sprintf("0x%x", 0x10+i*nandChunkBlocks) }

// nandChunkFilename is backupFilename with the piece number before ".bin".
func (c *camera) nandChunkFilename(i int) string {
	return strings.TrimSuffix(c.backupFilename(), ".bin") + fmt.Sprintf("-%02d.bin", i)
}

func (c *camera) nandBackup() []string {
	la := c.soc.LoadAddress
	text := []string{doNotPaste, "printenv ethaddr"}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff "+nandChunkHex)
	if c.sdWifi() {
		text = append(text, "mmc dev 0; mmc erase 0x10 "+c.flashSizeBlocks())
	}
	for i := 0; i < nandChunks; i++ {
		read := "nand read " + la + " " + nandChunkOffset(i) + " " + nandChunkHex + " && "
		if c.sdWifi() {
			text = append(text, read+"mmc write "+la+" "+nandChunkBlock(i)+" "+fmt.Sprintf("0x%x", nandChunkBlocks))
		} else {
			text = append(text, read+"tftpput "+la+" "+nandChunkHex+" "+c.nandChunkFilename(i))
		}
	}
	if c.sdWifi() {
		text = append(text,
			"",
			"# Use the following command to copy the pieces to files on a PC",
			"# (replace /dev/sdc with your SD card device):",
			fmt.Sprintf("# for i in $(seq 0 %d); do sudo dd bs=512 skip=$((16 + i * %d)) count=%d if=/dev/sdc of=./%s-$(printf %%02d $i).bin; done",
				nandChunks-1, nandChunkBlocks, nandChunkBlocks, strings.TrimSuffix(c.backupFilename(), ".bin")))
	} else {
		text = append(text,
			"# if there is no tftpput but tftp then use it instead, with the file name",
			"# before the size (the third argument is what makes tftp upload):",
			"# nand read "+la+" 0x0 "+nandChunkHex+" && tftp "+la+" "+c.nandChunkFilename(0)+" "+nandChunkHex)
	}
	return text
}

func (c *camera) flashingEverything() []string {
	la := c.soc.LoadAddress
	fw := c.fullImageFilename()
	text := []string{doNotPaste, c.env(), "mw.b " + la + " 0xff " + c.stagingSizeHex()}
	text = c.unlock(text)
	// The full image holds the UBI image, so on the UBI-only layout it goes
	// on with write.trimffs like rootfs.ubi does on its own.
	w := c.writeCmd()
	if c.ubi() {
		if c.sdWifi() {
			text = append(text, c.ubiFlash("fatload mmc 0:1 "+la+" "+fw, true))
		} else {
			text = append(text,
				c.ubiFlash("tftpboot "+la+" "+fw, true),
				"# if there is no tftpboot but tftp then run this instead",
				c.ubiFlash("tftp "+la+" "+fw, true))
		}
		return append(text, "reset")
	}
	if c.sdWifi() {
		text = append(text, c.guardedWrite("fatload mmc 0:1 "+la+" "+fw, "0x0", c.flashSizeHex(), w, "${filesize}"))
	} else {
		text = append(text,
			c.guardedWrite("tftpboot "+la+" "+fw, "0x0", c.flashSizeHex(), w, "${filesize}"),
			"# if there is no tftpboot but tftp then run this instead",
			c.guardedWrite("tftp "+la+" "+fw, "0x0", c.flashSizeHex(), w, "${filesize}"))
	}
	return append(text, "reset")
}

func (c *camera) flashingUboot() []string {
	la := c.soc.LoadAddress
	ub := c.bootloader()
	size := c.bootSize()
	ws := c.writeSizeFor(size)
	text := []string{doNotPaste}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff "+size)
	text = c.unlock(text)
	if c.sdWifi() {
		text = append(text, c.guardedFlash("fatload mmc 0:1 "+la+" "+ub, "0x0", size, ws))
	} else {
		text = append(text,
			c.guardedFlash("tftpboot "+la+" "+ub, "0x0", size, ws),
			"# if there is no tftpboot but tftp then run this instead",
			c.guardedFlash("tftp "+la+" "+ub, "0x0", size, ws))
	}
	return append(text, "reset")
}

func (c *camera) flashingLinux() []string {
	la := c.soc.LoadAddress
	s := c.bootloaderMacroSuffix()
	text := []string{doNotPaste}
	if c.iface != "wifi" {
		text = append(text, c.env())
		if c.macAddressCommand() {
			text = append(text, "setenv ethaddr "+c.mac)
		}
		text = append(text, "saveenv")
	}
	if c.ubi() {
		// One file, one region: the whole UBI device is erased and the image
		// written with write.trimffs, never a plain write (see writeCmd). No
		// staging blank: the write is ${filesize} long, and blanking the
		// region would be 127 MiB of RAM the camera may not have.
		ubi := "rootfs.ubi." + c.board
		if c.sdWifi() {
			text = append(text, c.ubiFlash("fatload mmc 0:1 "+la+" "+ubi, false), "")
		} else {
			text = append(text,
				c.ubiFlash("tftpboot "+la+" "+ubi, false),
				"# if there is no tftpboot but tftp then run this instead",
				c.ubiFlash("tftp "+la+" "+ubi, false))
		}
		return append(text, "reset")
	}
	if c.sdWifi() {
		text = append(text, "mw.b "+la+" 0xff 0x200000")
		text = c.unlock(text)
		text = append(text, c.guardedFlash("fatload mmc 0:1 "+la+" uImage."+c.board,
			c.kernelOffset(), c.kernelMaxSize(), "${filesize}"), "")
		text = append(text, "mw.b "+la+" 0xff 0x500000")
		text = c.unlock(text)
		text = append(text, c.guardedFlash("fatload mmc 0:1 "+la+" rootfs.squashfs."+c.board,
			c.rootfsOffset(), c.rootfsMaxSize(), "${filesize}"), "")
	} else {
		text = append(text, "run uk"+s+"; run ur"+s)
	}
	if c.flashType != "nand" {
		text = append(text, "sf erase "+c.overlayOffset()+" "+c.overlayMaxSize())
	}
	return append(text, "reset")
}

func (c *camera) restoreFromBackup() []string {
	if c.nand() {
		return c.nandRestore()
	}
	la := c.soc.LoadAddress
	ws := c.writeSizeFor(c.flashSizeHex())
	text := []string{doNotPaste}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff "+c.stagingSizeHex())
	text = c.unlock(text)
	if c.sdWifi() {
		text = append(text, c.guardedFlash("fatload mmc 0:1 "+la+" "+c.backupFilename(), "0x0", c.flashSizeHex(), ws))
	} else {
		text = append(text, c.guardedFlash("tftpboot "+la+" "+c.backupFilename(), "0x0", c.flashSizeHex(), ws))
	}
	return text
}

// nandRestore writes the nandBackup pieces back in order. Each one erases
// its own range and the next piece's: a piece that skipped bad blocks on the
// way out runs into the next range on the way back, and that has to be
// erased before it is written. The next piece erases it again and writes the
// same data there, since its own read started at the same good blocks.
func (c *camera) nandRestore() []string {
	la := c.soc.LoadAddress
	text := []string{doNotPaste}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff "+nandChunkHex)
	for i := 0; i < nandChunks; i++ {
		erase := fmt.Sprintf("0x%x", 2*nandChunkSize)
		if i == nandChunks-1 {
			erase = nandChunkHex
		}
		transfer := "tftpboot " + la + " " + c.nandChunkFilename(i)
		if c.sdWifi() {
			transfer = "fatload mmc 0:1 " + la + " " + c.nandChunkFilename(i)
		}
		text = append(text, c.guardedFlash(transfer, nandChunkOffset(i), erase, nandChunkHex))
	}
	return text
}
