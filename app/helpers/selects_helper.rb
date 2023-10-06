# frozen_string_literal: true

module SelectsHelper
  def list_of_network_interfaces_for_select
    Camera::NET_IFACE.map do |v|
      [t("net_iface.#{v}"), v]
    end
  end

  def list_of_flash_type_sizes_for_select
    Camera::FLASH_CHIP.map do |v|
      [t("flash_chip.#{v}"), v]
    end
  end

  def list_of_firmware_versions_for_select
    data = Camera::FW_VERSION.dup
    data.delete('venc') unless @camera.soc.vendor.name.eql?('Goke') && @camera.soc.model.in?(%w[GK7205V200 GK7205210 GK7205V300])
    data.map do |v|
      [t("firmware.version.#{v}"), v]
    end
  end

  def list_of_sd_card_for_select
    Camera::SD_CARD.map do |v|
      [t("sd_card.#{v}"), v]
    end
  end

  def list_of_socs_for_select
    Soc.all.map { |x| [x.full_name, x.id] }
  end

  def list_of_stages_for_select
    Soc::STATUS.map do |key,value|
      ["#{key.upcase}: #{value}", key]
    end.freeze
  end
end
