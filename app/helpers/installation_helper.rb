module InstallationHelper
  def list_of_commands(text)
    content_tag "pre", text.join("<br>").html_safe, class: "bg-light p-4"
  end

  def do_not_copy_paste
    content_tag "span", "# Enter commands line by line! Do not copy and paste multiple lines at once!", class: "text-danger"
  end

  def firmware_backup(c)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?("wifi")
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}; sf probe 0; sf read #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    text << "tftpput #{c.soc.load_address} #{c.flash_size_hex} #{c.backup_filename}"
    list_of_commands text
  end

  def flashing_everything(c)
    fw_filename = "openipc-#{c.soc.model_downcase}-#{c.firmware_version}-#{c.flash_size}mb.bin"
    text = []
    text << do_not_copy_paste
    text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}; tftpboot #{c.soc.load_address} #{fw_filename}"
    text << "sf probe 0; sf erase 0x0 #{c.flash_size_hex}; sf write #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    text << "reset"
    list_of_commands text
  end

  def flashing_uboot(c)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?("wifi")
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff 0x50000; tftpboot #{c.soc.load_address} #{c.soc.uboot_filename}"
    if c.flash_type.eql?("nand")
      text << "nand erase 0x0 0x50000; nand write #{c.soc.load_address} 0x0 0x50000"
    else
      text << "sf probe 0; sf erase 0x0 0x50000; sf write #{c.soc.load_address} 0x0 0x50000"
    end
    text << "reset"
    list_of_commands text
  end

  def flashing_linux(c, c2)
    text = []
    text << do_not_copy_paste
    unless c.network_interface.eql?("wifi")
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
      text << "setenv ethaddr #{c.camera_mac_address}"
      text << "saveenv"
    end
    text << "run uk#{c2}; run ur#{c2}"
    text << "sf erase 0x750000 0xb0000" if c2.eql?("nor8m")
    text << "reset"
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
    unless c.network_interface.eql?("wifi")
      text << "setenv ipaddr #{c.camera_ip_address}; setenv serverip #{c.server_ip_address}"
    end
    text << "mw.b #{c.soc.load_address} 0xff #{c.flash_size_hex}; tftpboot #{c.soc.load_address} #{c.backup_filename}"
    text << "sf probe 0; sf erase 0x0 #{c.flash_size_hex}; sf write #{c.soc.load_address} 0x0 #{c.flash_size_hex}"
    list_of_commands text
  end
end
