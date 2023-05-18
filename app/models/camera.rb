# frozen_string_literal: true

class Camera
  include ActiveModel::Model
  include ActiveModel::Validations

  FW_VERSION = %w[lite ultimate fpv].freeze
  FLASH_CHIP = %w[nor8m nor16m nor32m nand].freeze
  NET_IFACE = %w[eth wifi both].freeze
  SD_CARD = %w[nosd sd].freeze

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
      # when 'nand'
      #   ??
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
      # when 'nand'
      #   ??
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
    # when 'nand'
    #   ??
    else
      '0x800000'
    end
  end

  def firmware_version_name
    @firmware_version_name ||= I18n.t("firmware.version.#{firmware_version}")
  end

  def kernel_max_size
    case firmware_version
    when 'lite'
      '0x200000'
    when 'ultimate'
      '0x300000'
    else
      '0x200000'
    end
  end

  def kernel_offset
    case firmware_version
    when 'lite'
      '0x50000'
    when 'ultimate'
      '0x50000'
    else
      '0x50000'
    end
  end

  def rootfs_max_size
    case firmware_version
    when 'lite'
      '0x500000'
    when 'ultimate'
      '0xA00000'
    else
      '0x500000'
    end
  end

  def rootfs_offset
    case firmware_version
    when 'lite'
      '0x250000'
    when 'ultimate'
      '0x350000'
    else
      '0x250000'
    end
  end

  def overlay_max_size
    case firmware_version
    when 'lite'
      '0xB0000'
    when 'ultimate'
      '0x2B0000'
    else
      '0xB0000'
    end
  end

  def overlay_offset
    case firmware_version
    when 'lite'
      '0x750000'
    when 'ultimate'
      '0xD50000'
    else
      '0x750000'
    end
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
