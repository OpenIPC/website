# frozen_string_literal: true

class Camera
  include ActiveModel::Model
  include ActiveModel::Validations

  # What this model will accept, rather than what any particular SoC offers --
  # Soc#available_releases answers that, and the form is built from it.
  # `fabricator` is gone: upstream publishes no fabricator build for any SoC and
  # never has. `neo` is here because it does exist, for seven boards.
  FW_VERSION = %w[lite ultimate neo].freeze
  FLASH_CHIP = %w[nor8m nor16m nor32m nand].freeze

  # How the chip is carved up, which is a different question from how big it
  # is. Every OpenIPC bootloader carries both mtdpartsnor8m and mtdpartsnor16m
  # and either can be run on a part large enough to hold it, so a 16MB chip can
  # perfectly well wear the 8MB layout -- and one flashed from the 8MB image
  # already does.
  #
  # The two were a single menu entry until a camera turned up that could not be
  # revived by a full reflash. Its chip was 16MB and the 8MB entry had been
  # chosen, so the instructions erased 0x0..0x800000 and stopped. Every layout
  # here ends `-(rootfs_data)`, meaning "to the end of the device", so the
  # overlay ran to 0x1000000 and the half of it above the erase survived intact.
  # /init mounts jffs2 and only reformats when that mount fails, so the old,
  # broken overlay came back every time.
  PARTITION_LAYOUT = %w[nor8m nor16m].freeze
  NET_IFACE = %w[eth wifi both].freeze
  SD_CARD = %w[nosd sd].freeze

  # NAND geometry. The parts OpenIPC ships NAND builds for are 1Gbit SPI-NAND:
  # 128MiB, 2KiB page, 128KiB erase block. The layout below is the HiSilicon /
  # Goke `mtdpartsubi` one -- 256k(boot),768k(wtf),3072k(kernel),-(ubi) -- as
  # read from a real `printenv` in OpenIPC/firmware#695. It is family specific,
  # not universal; SigmaStar and Rockchip carry their kernel inside the UBI
  # image instead and are refused by Firmware#generate.
  NAND_SIZE_HEX = '0x8000000'
  NAND_KERNEL_OFFSET = '0x100000'
  NAND_KERNEL_MAX_SIZE = '0x300000'
  NAND_ROOTFS_OFFSET = '0x400000'
  NAND_ROOTFS_MAX_SIZE = '0x7c00000'

  # What `mw.b` pre-fills before a transfer, i.e. how much RAM the image needs.
  # It is the flash size on NOR, but it cannot be on NAND: the chip is 128MiB
  # and these cameras have 64MB of RAM. A NAND full image stops after the UBI
  # payload, so it is at most 0x400000 + the 16384KB rootfs.ubi cap the
  # firmware Makefile enforces = 20MB exactly. 24MB leaves headroom for a cap
  # bump and still fits at the load address.
  NAND_STAGING_SIZE_HEX = '0x1800000'

  attr_accessor :soc_id, :needs_instruction, :flash_type, :sd_card_slot,
                :network_interface, :camera_ip_address, :server_ip_address,
                :firmware_version, :camera_mac_address, :soc, :backup_filename
  attr_writer :partition_layout

  validates :soc_id, presence: true
  validates :flash_type, presence: true
  # Against what the SoC actually has when there is one, so a hand-edited form
  # cannot ask for an edition upstream does not build. FW_VERSION is the answer
  # when there is no SoC to ask, and when the index cannot be read
  # available_releases returns the known list, so this never becomes unsatisfiable.
  validates :firmware_version, inclusion: { in: ->(camera) { camera.permitted_firmware_versions } }
  validates :camera_mac_address, format: { with: MAC_ADDRESS_FORMAT }
  validates :camera_ip_address, format: { with: IP_ADDRESS_FORMAT }
  validates :server_ip_address, format: { with: IP_ADDRESS_FORMAT }

  def flash_type_name
    I18n.t("flash_chip.#{flash_type}")
  end

  def camera_ip_address
    @camera_ip_address ||= '192.168.1.10'
  end

  def server_ip_address
    @server_ip_address ||= '192.168.1.254'
  end

  def backup_filename
    @backup_filename ||= "backup-#{model.downcase}-#{@flash_type}.bin"
  end

  def nand?
    @flash_type.eql?('nand')
  end

  def flash_type_type
    case @flash_type
    when 'nor8m', 'nor16m', 'nor32m'
      'nor'
    when 'nand'
      'nand'
    else
      ''
    end
  end

  def flash_size
    case @flash_type
    when 'nor8m'
      8
    when 'nor16m'
      16
    when 'nor32m'
      32
    when 'nand'
      128
    else
      8
    end
  end

  def flash_size_blocks
    case @flash_type
    when 'nor8m'
      '0x4000'
    when 'nor16m'
      '0x8000'
    when 'nor32m'
      '0x16000'
    when 'nand'
      '0x40000'
    else
      '0x4000'
    end
  end

  def flash_size_hex
    case @flash_type
    when 'nor8m'
      '0x800000'
    when 'nor16m'
      '0x1000000'
    when 'nor32m'
      '0x2000000'
    when 'nand'
      NAND_SIZE_HEX
    else
      '0x800000'
    end
  end

  def staging_size_hex
    nand? ? NAND_STAGING_SIZE_HEX : flash_size_hex
  end

  def flash_size_sectors
    case @flash_type
    when 'nor8m'
      '16384'
    when 'nor16m'
      '32768'
    when 'nor32m'
      '65536'
    when 'nand'
      '262144'
    end
  end

  # What this SoC has for the flash type actually chosen. The union across flash
  # types would let a NAND-only edition through for a NOR part and back again:
  # sixteen boards have a NAND build, eleven of those are Ultimate-only and five
  # are Lite-only, so the two lists genuinely differ.
  #
  # Unioning with FW_VERSION would have made the rule say nothing at all --
  # Ultimate permitted for the 42 SoCs upstream does not build it for, which is
  # the thing being fixed.
  # Move to an edition upstream publishes for the chosen flash type, and answer
  # what was asked for so the caller can say what it did. nil when nothing
  # needed changing.
  #
  # Nothing calls valid? on a Camera -- these pages are rendered, never saved --
  # and every field arrives from the request, so without this a hand-edited
  # query string produces a full page of installation steps for a tarball that
  # was never built. The same shape as the 8MB downgrade that has always been
  # here, applied to what exists rather than to what fits.
  def use_published_release!
    return nil if soc.nil?

    available = soc.available_releases(flash_type_type)
    return nil if available.empty? || available.include?(firmware_version)

    asked = firmware_version
    self.firmware_version = available.first
    @firmware_version_name = nil
    asked
  end

  def permitted_firmware_versions
    return FW_VERSION if soc.nil?

    soc.available_releases(flash_type_type).presence || soc.offerable_releases
  end

  # The same default as the selector. Without it an edition upstream publishes
  # that this site has no translation for is picked from the menu by its proper
  # name and then rendered as a translation_missing span everywhere else on the
  # installation page.
  def firmware_version_name
    @firmware_version_name ||= I18n.t("firmware.version.#{firmware_version}",
                                      default: firmware_version.to_s.capitalize)
  end

  # Which mtdparts this camera is being given. Defaults to the one that matches
  # the chip, so a visitor who never opens the second menu gets exactly what
  # this page has always produced, and refuses a layout the chip cannot hold --
  # the 16MB one ends at 0xD50000, which is past the end of an 8MB part.
  #
  # Unrecognised is the same as unset. Like every other field here it can arrive
  # from a query string, and nothing calls valid? on a Camera.
  def partition_layout
    return 'nand' if nand?
    return default_partition_layout unless @partition_layout.in?(PARTITION_LAYOUT)
    return default_partition_layout unless layout_fits_chip?(@partition_layout)

    @partition_layout
  end

  def default_partition_layout
    return 'nand' if nand?

    flash_size <= 8 ? 'nor8m' : 'nor16m'
  end

  # The 8MB layout fits anything; the 16MB one needs a 16MB part.
  def layout_fits_chip?(layout)
    layout.eql?('nor8m') || flash_size >= 16
  end

  def layout_size
    partition_layout.eql?('nor8m') ? 8 : 16
  end

  # `nand` is not in PARTITION_LAYOUT and cannot be chosen: NAND has one layout,
  # mtdpartsubi, and the menu is hidden for it. It is what partition_layout
  # answers there so that the name doubles as the suffix of the bootloader
  # macros -- `uknand`, `urnand`, `setnand`, and `uknor8m` and friends on NOR.
  # That is also why a 32MB part has always been told to `run setnor16m`: there
  # is no mtdpartsnor32m in any bootloader upstream ships.
  def partition_layout_name
    I18n.t("flash_layout.#{partition_layout}")
  end

  # Whether this camera's bootloader carries one NOR mtdparts string and
  # unsuffixed macros -- see FlashLayout. NAND is a separate environment with
  # its own uknand/urnand/setnand and is not affected either way.
  def fixed_mtdparts?
    !nand? && FlashLayout.fixed_mtdparts?(soc&.vendor&.name)
  end

  # The suffix this camera's bootloader macros actually carry. `uknor8m` and
  # friends on HiSilicon and Goke, `uknand` on NAND, and plain `uknor`/`urnor`
  # on SigmaStar and Ingenic, whose environment has no suffixed macro at all --
  # the page has been telling those cameras to `run uknor16m` since it first
  # had an expert section, and U-Boot has been answering `## Error: "uknor16m"
  # not defined` and flashing nothing.
  def bootloader_macro_suffix
    return 'nand' if nand?
    return 'nor' if fixed_mtdparts?

    partition_layout
  end

  # Whether the bootloader already boots this layout without being told. Every
  # one of them defaults to the 8MB partitions, and a full-image flash leaves
  # the env erased, so that default is what a freshly flashed camera comes up
  # with.
  #
  # NAND is not one of them: mtdpartsubi is not a default anything falls back
  # to, and a NAND camera has always been told to `run setnand` after a full
  # image like it is now.
  def default_bootloader_layout?
    !nand? && layout_size <= 8
  end

  # What to run to put the bootloader on this layout, if anything.
  #
  # HiSilicon and Goke have a macro for it. SigmaStar and Ingenic do not: their
  # mtdparts is one string with ${rootmtd} in it, saved unexpanded and expanded
  # at boot by `cmdnor`, so the layout is changed by setting that variable and
  # the erase length that goes with it. Empty when there is nothing to change,
  # which is what rootmtd=5120k already is.
  def layout_commands
    return ["run set#{bootloader_macro_suffix}"] unless fixed_mtdparts?
    return [] if default_bootloader_layout?

    ["setenv rootmtd #{rootfs_max_size.to_i(16) / 1024}k; setenv rootsize #{rootfs_max_size}",
     'saveenv', 'reset']
  end

  # The bootloader variables the instructions above actually named, for the hint
  # that tells the reader to go and look them up. Built from the same suffix the
  # commands are, so the two cannot drift -- and without a `set…` entry where no
  # such variable exists, since the reader would not find it in their printenv.
  def bootloader_variables
    names = %w[uk ur].map { |prefix| "#{prefix}#{bootloader_macro_suffix}" }
    names << "set#{bootloader_macro_suffix}" unless fixed_mtdparts?
    names
  end

  # The NOR numbers come from FlashLayout, which reads them off the bootloader
  # environment. They used to be spelled out here keyed on firmware_version,
  # which agreed with the bootloader only for 8MB+Lite and 16MB+Ultimate; see
  # FlashLayout for what that cost on 16MB.
  #
  # Keyed on the layout, not on the chip. The two agree for every combination
  # the menu offered before it grew a second field, and the whole point of the
  # second field is the ones where they do not.
  #
  # The vendor goes with it because two of them have a bootloader whose rootfs
  # offset does not move between layouts. Without it a 16MB SigmaStar or Ingenic
  # camera is handed 0x350000, which its bootloader never reads.
  def nor_layout
    FlashLayout.nor(layout_size, soc&.vendor&.name)
  end

  def kernel_max_size
    return NAND_KERNEL_MAX_SIZE if nand?

    hex nor_layout[:kernel_max_size]
  end

  def kernel_offset
    return NAND_KERNEL_OFFSET if nand?

    hex nor_layout[:kernel_offset]
  end

  def rootfs_max_size
    return NAND_ROOTFS_MAX_SIZE if nand?

    hex nor_layout[:rootfs_max_size]
  end

  def rootfs_offset
    return NAND_ROOTFS_OFFSET if nand?

    hex nor_layout[:rootfs_offset]
  end

  # Guards the arithmetic that used to render `nand erase 0xD50000 0x-550000`:
  # NAND fell through to the 8MB NOR default, so flash_size_hex was smaller
  # than overlay_offset and the subtraction went negative. U-Boot's
  # simple_strtoul stops at the '-', silently turning the size into 0.
  def overlay_max_size
    size = flash_size_hex.to_i(16) - overlay_offset.to_i(16)
    raise StandardError, "overlay_offset #{overlay_offset} is beyond flash size #{flash_size_hex}" if size <= 0

    "0x#{size.to_s(16)}"
  end

  # rootfs_data: everything past the rootfs partition. Keyed on the chip, like
  # the rest of the layout -- this is the value that used to erase into a
  # freshly written 16MB rootfs.
  def overlay_offset
    hex nor_layout[:overlay_offset]
  end

  # These are pasted into U-Boot verbatim, so the 0x prefix is not optional.
  # The case of the digits is: simple_strtoul takes either, and this class is
  # not consistent about it -- the offsets below come out upper-case and
  # overlay_max_size computes its length lower-case. Matching the literals this
  # replaced, rather than changing what every existing page renders.
  def hex(value)
    format('0x%X', value)
  end

  def permalink
    [
      '?mac=', camera_mac_address.gsub(':', '-'),
      '&cip=', camera_ip_address,
      '&sip=', server_ip_address,
      '&net=', network_interface,
      '&rom=', flash_type,
      # The layout is written whether or not it differs from the chip's own, so
      # a link says what it means rather than leaning on today's default.
      '&part=', partition_layout,
      # `ver`, not `var`. This emitted `var` while show has always read `ver`,
      # so the edition was the one field the permanent link dropped: reopening
      # a link for Ultimate on a 32MB chip came back as Lite. show still
      # accepts `var` too, because every link anyone has shared carries it.
      '&ver=', firmware_version,
      '&sd=', sd_card_slot
    ].join('').html_safe
  end
end
