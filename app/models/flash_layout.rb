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

  # SigmaStar and Ingenic do not have that pair, and the bootloaders that do are
  # not the ones those SoCs ship.
  #
  # OpenIPC/firmware's .github/workflows/uboot.yml is what builds the binaries
  # this site links. It clones openipc/u-boot-sigmastar and openipc/u-boot-
  # ingenic and runs their build.sh; the SigmaStar one's spinor loop is
  # `ssc377 ssc377d ssc377de ssc377qe ssc378de ssc378qe` against
  # include/configs/infinity6c.h, which includes configs/sstar-common.h. That
  # header is the entire environment every SSC3xx NOR camera boots with:
  #
  #   kernaddr=0x50000   kernsize=0x200000
  #   rootaddr=0x250000  rootsize=0x500000   rootmtd=5120k
  #   uknor / urnor / ubnor            <- no size suffix, and no setnor* at all
  #   CONFIG_BOOTARGS "... mtdparts=NOR_FLASH:256k(boot),64k(env),2048k(kernel),
  #                        ${rootmtd}(rootfs),-(rootfs_data) ..."
  #
  # A repo-wide grep for uknor8m|uknor16m|urnor8m|urnor16m|setnor8m|setnor16m|
  # mtdpartsnor returns nothing in either repo; u-boot-ingenic's
  # include/configs/isvp_common.h carries the same unsuffixed uknor/urnor.
  # u-boot-msc313e, u-boot-t20 and u-boot-t40 do define the suffixed pair, which
  # is what the note this replaces was reading -- but no released binary is
  # built from them, so no camera runs them.
  #
  # So these two have one mtdparts string, the kernel partition is 2048k inside
  # it, and the only thing that varies is ${rootmtd}. The rootfs starts at
  # 0x250000 at every chip size; what a larger chip buys is a longer rootfs, not
  # one further up.
  #
  # Handing them NOR[16] put the rootfs at 0x350000, which is where the images
  # openipc.org serves today for ssc377qe have it: layout=16 carries "hsqs" at
  # 0x350000 and 0xff at 0x250000, and its env region at 0x40000 is blank, so
  # the camera comes up on the compiled-in bootargs, looks for the rootfs at
  # 0x250000 and panics on root mount. layout=8 has it at 0x250000 and boots.
  FIXED_MTDPARTS_VENDORS = %w[SigmaStar Ingenic].freeze

  # The same two questions the table above answers, for a bootloader whose
  # rootfs cannot move. 16 is not a different partition map, it is `rootmtd`
  # and the erase length that goes with it set to 10240k -- which is also the
  # only way an Ultimate rootfs fits: 7832KB on ssc338q, 7252KB on ssc30kq and
  # 6772KB on t31, against the 5120KB the default leaves.
  FIXED_MTDPARTS_NOR = {
    8 => { kernel_offset: 0x50000, kernel_max_size: 0x200000,
           rootfs_offset: 0x250000, rootfs_max_size: 0x500000,
           overlay_offset: 0x750000 }.freeze,
    16 => { kernel_offset: 0x50000, kernel_max_size: 0x200000,
            rootfs_offset: 0x250000, rootfs_max_size: 0xA00000,
            overlay_offset: 0xC50000 }.freeze
  }.freeze

  # Whether this vendor's NOR bootloader has one mtdparts string rather than a
  # mtdpartsnor8m/mtdpartsnor16m pair to switch between.
  def self.fixed_mtdparts?(vendor_name)
    FIXED_MTDPARTS_VENDORS.include?(vendor_name.to_s)
  end

  # The chip size decides which entry, and the vendor decides which table. What
  # has to travel with the 16MB entry either way is the instruction to put the
  # bootloader on it: all of these default to the 8MB partitions -- mtdparts on
  # HiSilicon and Goke, rootmtd=5120k on SigmaStar and Ingenic -- and flashing a
  # full image leaves the env erased, so that default is what boots. See
  # Camera#layout_commands for what each family is told to run.
  def self.nor(flash_size_mb, vendor_name = nil)
    table = fixed_mtdparts?(vendor_name) ? FIXED_MTDPARTS_NOR : NOR
    table[flash_size_mb.to_i <= 8 ? 8 : 16]
  end
end
