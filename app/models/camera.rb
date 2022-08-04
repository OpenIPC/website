class Camera
  include ActiveModel::Model
  include ActiveModel::Validations

  FLASH_CHIP = %w[nor8m nor16m nor32m nand].freeze
  NET_IFACE = %w[eth wifi both].freeze
  SD_CARD = %w[nosd sd].freeze

  attr_accessor :soc_id, :needs_instruction, :flash_type, :sd_card_slot, :network_interface, :camera_ip_address, :server_ip_address

  validates :soc_id, presence: true
  validates :flash_type, presence: true
  validates :camera_ip_address, format: { with: IP_ADDRESS_FORMAT }
  validates :server_ip_address, format: { with: IP_ADDRESS_FORMAT }

  def flash_type_name
    I18n.t("flash_chip.#{flash_type}")
  end

  def camera_ip_address
    @camera_ip_address ||= "192.168.1.10"
  end

  def server_ip_address
    @server_ip_address ||= "192.168.1.254"
  end

  def backup_filename
    @backup_filename ||= "backup-#{model.downcase}-#{@flash_type}.bin"
  end

  def flash_size_hex
    @flash_size_hex ||= case @flash_type
                        when "nor8m"
                          "0x800000"
                        when "nor16m"
                          "0x1000000"
                        when "nor32m"
                          "0x2000000"
                        # when "nand"
                        #   "0x800000"
                        else
                          "0x800000"
                        end
  end

  # if @camera.flash_type.eql?("nor16m")
  #   @camera.flash_size_hex = "0x1000000" # 16M
  # else
  #   @camera.flash_size_hex = "0x800000" # 8M
  # end
end
