package wizard

import "strings"

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
	cmd := "sf"
	if c.flashType == "nand" {
		cmd = "nand"
	}
	return transfer + " && " + cmd + " erase " + offset + " " + eraseSize + " && " + cmd +
		" write " + c.soc.LoadAddress + " " + offset + " " + writeSize
}

func (c *camera) writeSizeFor(fixed string) string {
	if c.flashType == "nand" {
		return fixed
	}
	return "${filesize}"
}

func (c *camera) firmwareBackup() []string {
	la := c.soc.LoadAddress
	text := []string{doNotPaste, "printenv ethaddr"}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff "+c.flashSizeHex())
	if c.flashType == "nand" {
		text = append(text, "nand read "+la+" 0x0 "+c.flashSizeHex())
	} else {
		text = append(text, "sf probe 0; sf read "+la+" 0x0 "+c.flashSizeHex())
	}
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

func (c *camera) flashingEverything() []string {
	la := c.soc.LoadAddress
	fw := c.fullImageFilename()
	text := []string{doNotPaste, c.env(), "mw.b " + la + " 0xff " + c.stagingSizeHex()}
	text = c.unlock(text)
	if c.sdWifi() {
		text = append(text, c.guardedFlash("fatload mmc 0:1 "+la+" "+fw, "0x0", c.flashSizeHex(), "${filesize}"))
	} else {
		text = append(text,
			c.guardedFlash("tftpboot "+la+" "+fw, "0x0", c.flashSizeHex(), "${filesize}"),
			"# if there is no tftpboot but tftp then run this instead",
			c.guardedFlash("tftp "+la+" "+fw, "0x0", c.flashSizeHex(), "${filesize}"))
	}
	return append(text, "reset")
}

func (c *camera) flashingUboot() []string {
	la := c.soc.LoadAddress
	ub := c.soc.UBootFilename
	ws := c.writeSizeFor("0x50000")
	text := []string{doNotPaste}
	if c.iface != "wifi" {
		text = append(text, c.env())
	}
	text = append(text, "mw.b "+la+" 0xff 0x50000")
	text = c.unlock(text)
	if c.sdWifi() {
		text = append(text, c.guardedFlash("fatload mmc 0:1 "+la+" "+ub, "0x0", "0x50000", ws))
	} else {
		text = append(text,
			c.guardedFlash("tftpboot "+la+" "+ub, "0x0", "0x50000", ws),
			"# if there is no tftpboot but tftp then run this instead",
			c.guardedFlash("tftp "+la+" "+ub, "0x0", "0x50000", ws))
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
