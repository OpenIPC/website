# frozen_string_literal: true

module SelectsHelper
  def list_of_network_interfaces_for_select
    Camera::NET_IFACE.map do |v|
      [t("net_iface.#{v}"), v]
    end
  end

  # NAND is offered for every SoC today, and upstream builds a NAND image for
  # sixteen boards. Choosing it anywhere else produced instructions for a
  # tarball that does not exist. A flash type upstream builds nothing for is
  # disabled rather than hidden, so the menu still shows what the part has.
  def list_of_flash_type_sizes_for_select
    availability = @camera.soc.release_availability

    Camera::FLASH_CHIP.map do |chip|
      flash_type = chip.start_with?('nand') ? 'nand' : 'nor'
      option = [t("flash_chip.#{chip}"), chip]
      availability[flash_type].empty? ? option + [{ disabled: true }] : option
    end
  end

  # Both layouts, always. Which of them a chip can hold is narrowed on the page,
  # beside the rule that narrows the editions -- an 8MB part cannot wear the
  # 16MB layout, whose rootfs partition ends at 0xD50000.
  def list_of_partition_layouts_for_select
    Camera::PARTITION_LAYOUT.map { |layout| [t("flash_layout.#{layout}"), layout] }
  end

  # Every SoC used to be offered lite, ultimate and fabricator whatever upstream
  # built. The list comes from the release index now -- the union across flash
  # types, because the flash type is chosen in the same form without a round
  # trip and the script on the page narrows it from there.
  #
  # The line removed here deleted 'venc' from a list that has never contained
  # it, guarded by a condition naming GK7205210, which is not a SoC. It had no
  # effect either way.
  #
  # The default label matters: `fabricator` has no translation in any locale, so
  # `t` rendered a translation_missing span inside an <option> on every SoC page
  # on the site. An edition upstream invents tomorrow gets its own name rather
  # than that.
  def list_of_firmware_versions_for_select
    @camera.soc.offerable_releases.map do |release|
      [t("firmware.version.#{release}", default: release.capitalize), release]
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
