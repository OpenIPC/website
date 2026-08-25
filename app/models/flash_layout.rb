# frozen_string_literal: true

# Where the parts of a NOR image live, so that the page telling someone what to
# flash and the generator building what they flash cannot disagree.
#
# Straight out of the bootloader environment OpenIPC ships. u-boot-hi3516ev200,
# u-boot-gk7205v200, u-boot-hi3516cv500 and u-boot-t20 all carry the same pair:
#
#   mtdpartsnor8m  = 256k(boot),64k(env),2048k(kernel),5120k(rootfs),-(rootfs_data)
#   mtdpartsnor16m = 256k(boot),64k(env),3072k(kernel),10240k(rootfs),-(rootfs_data)
#
#   uknor8m  : sf erase 0x50000  0x200000    urnor8m  : sf erase 0x250000 0x500000
#   uknor16m : sf erase 0x50000  0x300000    urnor16m : sf erase 0x350000 0xa00000
#
# Note what those are keyed on: the size of the chip. Camera used to hold these
# same numbers keyed on the firmware edition instead, which agreed with the
# bootloader only for 8MB+Lite and 16MB+Ultimate. The wizard forces Lite on
# 16MB, so 16MB+Lite -- what every 16MB NOR camera actually gets -- was handed
# the 8MB layout. On the network path `run urnor16m` writes the rootfs to
# 0x350000..0xd50000 and the page then said `sf erase 0x750000`, 733,184 bytes
# into what had just been written. On the SD-card path it wrote the rootfs to
# 0x250000, where the kernel's mtdparts does not expect to find it.
#
# There is no mtdpartsnor32m anywhere upstream. Cameras::SocsController maps
# nor32m onto the nor16m commands for that reason, and this follows it.
class FlashLayout
  NOR = {
    8 => { kernel_offset: 0x50000, kernel_max_size: 0x200000,
           rootfs_offset: 0x250000, rootfs_max_size: 0x500000,
           overlay_offset: 0x750000 }.freeze,
    16 => { kernel_offset: 0x50000, kernel_max_size: 0x300000,
            rootfs_offset: 0x350000, rootfs_max_size: 0xA00000,
            overlay_offset: 0xD50000 }.freeze
  }.freeze

  # SigmaStar and Ingenic used to be pinned to the 8MB offsets whatever chip was
  # chosen, on the grounds that their bootloaders defined no uknor/urnor macros
  # to check against. They do. The repositories checked were u-boot-sigmastar
  # and u-boot-ingenic, which are not what those SoCs ship; the real ones are
  # per-SoC, and u-boot-t20, u-boot-t40 and u-boot-msc313e all carry the same
  # pair as the Hisilicon and Goke bootloaders above, down to the byte:
  #
  #   mtdpartsnor16m = 256k(boot),64k(env),3072k(kernel),10240k(rootfs),-(rootfs_data)
  #   uknor16m : sf erase 0x50000 0x300000    urnor16m : sf erase 0x350000 0xa00000
  #
  # Only the mtd device name differs -- jz_sfc, NOR_FLASH, sfc.
  #
  # The pin also could not survive Ultimate on 16MB. Ultimate's NOR rootfs is
  # 7820KB on ssc338q and 6752KB on t31, and the 8MB layout gives rootfs 5120KB.
  # There is no arrangement in which those two vendors offer Ultimate on a 16MB
  # chip and keep the 8MB geometry.
  #
  # This is the one place the chip size decides, for every vendor. What has to
  # travel with it is the instruction to run `setnor16m`: every one of these
  # bootloaders defaults mtdparts to the 8MB layout, and flashing a full image
  # leaves the env erased, so a 16MB camera that is never told to switch boots
  # with 8MB partitions. update.html.erb used to suppress that instruction for
  # these same two vendors and no longer does.
  def self.nor(flash_size_mb)
    flash_size_mb.to_i <= 8 ? NOR[8] : NOR[16]
  end
end
