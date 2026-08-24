# frozen_string_literal: true

class Camera
  include ActiveModel::Model
  include ActiveModel::Validations

  FW_VERSION = %w[lite ultimate fabricator].freeze
  FLASH_CHIP = %w[nor8m nor16m nor32m nand].freeze
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

  validates :soc_id, presence: true
  validates :flash_type, presence: true
  validates :firmware_version, inclusion: { in: FW_VERSION }
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

  def firmware_version_name
    @firmware_version_name ||= I18n.t("firmware.version.#{firmware_version}")
  end

  # The NOR numbers come from FlashLayout, which reads them off the bootloader
  # environment. They used to be spelled out here keyed on firmware_version,
  # which agreed with the bootloader only for 8MB+Lite and 16MB+Ultimate; see
  # FlashLayout for what that cost on 16MB.
  def nor_layout
    FlashLayout.nor(flash_size, soc&.vendor&.name)
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

  # The rest of this class renders offsets as upper-case hex strings, and the
  # installation commands are pasted into U-Boot verbatim, so the shape matters.
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
      '&var=', firmware_version,
      '&sd=', sd_card_slot
    ].join('').html_safe
  end
end
