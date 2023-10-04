# frozen_string_literal: true

module InstallationHelper
  def list_of_commands(text)
    content_tag 'pre', text.join('<br>').html_safe, class: 'bg-light p-4'
  end

  def do_not_copy_paste
    content_tag 'span', '# Enter commands line by line! Do not copy and paste multiple lines at once!', class: 'text-danger'
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
      text << "tftp #{c.soc.load_address} #{c.backup_filename} #{c.flash_size_hex}"
    end
    list_of_commands text
  end

  def flashing_everything(c)
    fw_filename = "openipc-#{c.soc.model_downcase}-#{c.firmware_version}-#{c.flash_size}mb.bin"
    text = []
    text << do_not_copy_paste
    text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}"
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "fatload mmc 0:1 #{c.soc.load_address} #{fw_filename}"
    else
      text << "tftpboot #{c.soc.load_address} #{fw_filename}"
      text << '# if there is no tftpboot but tftp then run this instead'
      text << "tftp #{c.soc.load_address} #{fw_filename}"
    end
    if c.flash_type.eql?('nand')
      text << "nand erase 0x0 #{c.flash_size_hex}; nand write #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    else
      text << 'sf probe 0; sf lock 0;'
      text << "sf erase 0x0 #{c.flash_size_hex}; sf write #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    end
    text << 'reset'
    list_of_commands text
  end

  def flashing_uboot(c)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff 0x50000"
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.uboot_filename}"
    else
      text << "tftpboot #{c.soc.load_address} #{c.soc.uboot_filename}"
      text << '# if there is no tftpboot but tftp then run this instead'
      text << "tftp #{c.soc.load_address} #{c.soc.uboot_filename}"
    end
    if c.flash_type.eql?('nand')
      text << "nand erase 0x0 0x50000; nand write #{c.soc.load_address} 0x0 0x50000"
    else
      text << 'sf probe 0; sf lock 0;'
      text << "sf erase 0x0 0x50000; sf write #{c.soc.load_address} 0x0 0x50000"
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
      text << "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.kernel_file}"
      if c.flash_type.eql?('nand')
        text << "nand erase #{c.kernel_offset} #{c.kernel_max_size}; nand write #{c.soc.load_address} #{c.kernel_offset} ${filesize}"
      else
        text << 'sf probe 0; sf lock 0;'
        text << "sf erase #{c.kernel_offset} #{c.kernel_max_size}; sf write #{c.soc.load_address} #{c.kernel_offset} ${filesize}"
      end
      text << ''
      text << "mw.b #{c.soc.load_address} 0xff 0x500000"
      text << "fatload mmc 0:1 #{c.soc.load_address} #{c.soc.rootfs_file}"
      if c.flash_type.eql?('nand')
        text << "nand erase #{c.rootfs_offset} #{c.rootfs_max_size}; nand write #{c.soc.load_address} #{c.rootfs_offset} ${filesize}"
      else
        text << 'sf probe 0; sf lock 0;'
        text << "sf erase #{c.rootfs_offset} #{c.rootfs_max_size}; sf write #{c.soc.load_address} #{c.rootfs_offset} ${filesize}"
      end
      text << ''
    else
      text << "run uk#{c2}; run ur#{c2}"
    end
    if c.flash_type.eql?('nand')
      text << "nand erase #{c.overlay_offset} #{c.overlay_max_size}"
    else
      text << "sf erase #{c.overlay_offset} #{c.overlay_max_size}"
    end
    text << 'reset'
    list_of_commands text
  end

  def preparing_environment(c2)
    text = []
    text << do_not_copy_paste
    text << "run set#{c2}"
    list_of_commands text
  end

  def restore_from_backup(c)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?('wifi')
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}"
    if c.sd_card_slot.eql?('sd') && c.network_interface.eql?('wifi')
      text << "fatload mmc 0:1 #{c.soc.load_address} #{c.backup_filename}"
    else
      text << "tftpboot #{c.soc.load_address} #{c.backup_filename}"
    end
    if c.flash_type.eql?('nand')
      text << "nand erase 0x0 #{c.flash_size_hex}; nand write #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    else
      text << 'sf probe 0; sf lock 0;'
      text << "sf erase 0x0 #{c.flash_size_hex}; sf write #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    end
    list_of_commands text
  end
end
