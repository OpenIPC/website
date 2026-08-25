# frozen_string_literal: true

module InstallationHelper
  def list_of_commands(text)
    content_tag 'pre', text.join('<br>').html_safe, class: 'bg-light p-4'
  end

  def do_not_copy_paste
    content_tag 'span', '# Enter commands line by line! Do not copy and paste multiple lines at once!', class: 'text-danger'
  end

  # `sf probe` has to run before any erase, and `sf lock 0` clears the status
  # register block protection some vendors arm. Kept as its own line rather
  # than chained into the flashing command: plenty of vendor U-Boots have no
  # `sf lock` subcommand at all, and a chain would abort on their error
  # instead of going on to flash.
  def unlock_flash(text, c)
    text << 'sf probe 0; sf lock 0;' unless c.flash_type.eql?('nand')
  end

  # Build one line that only erases if the transfer before it succeeded.
  #
  # Both halves matter. U-Boot's `&&` stops a failed `tftpboot`/`tftp`/
  # `fatload` from reaching the erase, and `${filesize}` bounds the write to
  # what actually arrived. Without them a transfer that fails, or one whose
  # load address was mistyped -- U-Boot's hex parser stops at the first
  # non-hex character, so `0mx82000000` silently becomes `0` -- still erased
  # the chip and wrote back the `mw.b` fill, putting 0xff over the
  # bootloader while reporting success. See OpenIPC/website#55 and the
  # camera it destroyed in OpenIPC/firmware#2299.
  #
  # It must stay a single line, because the block above it tells the user to
  # enter commands one at a time.
  def guarded_flash(c, transfer, offset, erase_size, write_size)
    cmd = c.flash_type.eql?('nand') ? 'nand' : 'sf'
    "#{transfer} && #{cmd} erase #{offset} #{erase_size} " \
      "&& #{cmd} write #{c.soc.load_address} #{offset} #{write_size}"
  end

  # NOR writes only what arrived. NAND keeps the fixed partition size it has
  # always used, since `${filesize}` is not guaranteed to be page-aligned.
  def write_size_for(c, fixed)
    c.flash_type.eql?('nand') ? fixed : '${filesize}'
  end

  def firmware_backup(c)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}"
    if c.flash_type.eql?('nand')
      text << "nand read #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    else
      text << "sf probe 0; sf read #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    end

    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "mmc dev 0; mmc erase 0x10 #{c.flash_size_blocks}; mmc write #{c.soc.load_address} 0x10 #{c.flash_size_blocks}"
      text << ""
      text << "# Use the following command to restore the backup to a file on a PC"
      text << "# (replace /dev/sdc with your SD card device):"
      text << "# sudo dd bs=512 skip=16 count=#{c.flash_size_sectors} if=/dev/sdc of=./fulldump.bin"
    else
      text << "tftpput #{c.soc.load_address} #{c.flash_size_hex} #{c.backup_filename}"
      text << '# if there is no tftpput but tftp then run this instead'
      text << '# (the third argument is what makes tftp upload rather than download)'
      text << "tftp #{c.soc.load_address} #{c.backup_filename} #{c.flash_size_hex}"
    end
    list_of_commands text
  end

  def flashing_everything(c)
    fw_filename = Firmware.filename_for(soc_model: c.soc.model_downcase, flash_type: c.flash_type_type,
                                        release: c.firmware_version, size: c.flash_size)
    # The full image is exactly the size it claims on NOR and page-aligned by
    # construction on NAND, so ${filesize} is always a safe write length here --
    # unlike the u-boot-only block below, where the binary is neither.
    write_size = '${filesize}'
    text = []
    text << do_not_copy_paste
    text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    text << "mw.b #{c.soc.load_address} 0xff #{c.staging_size_hex}"
    unlock_flash text, c
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{fw_filename}",
                            '0x0', c.flash_size_hex, write_size)
    else
      text << guarded_flash(c, "tftpboot #{c.soc.load_address} #{fw_filename}",
                            '0x0', c.flash_size_hex, write_size)
      text << '# if there is no tftpboot but tftp then run this instead'
      text << guarded_flash(c, "tftp #{c.soc.load_address} #{fw_filename}",
                            '0x0', c.flash_size_hex, write_size)
    end
    text << 'reset'
    list_of_commands text
  end

  def flashing_uboot(c)
    write_size = write_size_for(c, '0x50000')
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff 0x50000"
    unlock_flash text, c
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.uboot_filename}",
                            '0x0', '0x50000', write_size)
    else
      text << guarded_flash(c, "tftpboot #{c.soc.load_address} #{c.soc.uboot_filename}",
                            '0x0', '0x50000', write_size)
      text << '# if there is no tftpboot but tftp then run this instead'
      text << guarded_flash(c, "tftp #{c.soc.load_address} #{c.soc.uboot_filename}",
                            '0x0', '0x50000', write_size)
    end
    text << 'reset'
    list_of_commands text
  end

  def flashing_linux(c, c2)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
      text << "setenv ethaddr #{c.camera_mac_address}"
      text << 'saveenv'
    end
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "mw.b #{c.soc.load_address} 0xff 0x200000"
      unlock_flash text, c
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.kernel_file}",
                            c.kernel_offset, c.kernel_max_size, '${filesize}')
      text << ''
      text << "mw.b #{c.soc.load_address} 0xff 0x500000"
      unlock_flash text, c
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.rootfs_file}",
                            c.rootfs_offset, c.rootfs_max_size, '${filesize}')
      text << ''
    else
      text << "run uk#{c2}; run ur#{c2}"
    end
    # No overlay erase on NAND: with mtdpartsubi everything past the kernel is
    # one `ubi` partition and rootfs_data is a volume inside it, so a raw erase
    # at an offset would cut into UBI. `urnand` already erases that whole
    # partition before writing. This is also what used to render the malformed
    # `nand erase 0xD50000 0x-550000`.
    text << "sf erase #{c.overlay_offset} #{c.overlay_max_size}" unless c.flash_type.eql?('nand')
    text << 'reset'
    list_of_commands text
  end

  # The three bootloader variables the instructions above actually named, for
  # the hint that tells the reader to go and look them up. preparing_environment
  # emits `run set…` and flashing_linux emits `run uk…; run ur…`, all from the
  # same flash_type_command, so building the hint from it too keeps the three
  # in step -- including the nor32m -> nor16m rewrite the controller does.
  #
  # It used to be a fixed `uknor*, urnor*, setnor*`, which named nothing a NAND
  # reader had been given and nothing they could find in their own printenv.
  def bootloader_variables_html(flash_type_command)
    safe_join(%w[uk ur set].map { |prefix| tag.code("#{prefix}#{flash_type_command}") }, ', ')
  end

  def preparing_environment(c2)
    text = []
    text << do_not_copy_paste
    text << "run set#{c2}"
    list_of_commands text
  end

  def restore_from_backup(c)
    write_size = write_size_for(c, c.flash_size_hex)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.staging_size_hex}"
    unlock_flash text, c
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << guarded_flash(c, "fatload mmc 0:1 #{c.soc.load_address} #{c.backup_filename}",
                            '0x0', c.flash_size_hex, write_size)
    else
      text << guarded_flash(c, "tftpboot #{c.soc.load_address} #{c.backup_filename}",
                            '0x0', c.flash_size_hex, write_size)
    end
    list_of_commands text
  end
end
