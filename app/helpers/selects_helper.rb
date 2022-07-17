module SelectsHelper
  def list_of_network_interfaces_for_select
    [['Camera only has Ethernet network', 'eth'],
     ['Camera only has Wireless network', 'wifi'],
     ['Camera has both Ethernet and Wireless networks', 'both']].freeze
  end

  def list_of_flash_type_sizes_for_select
    [['NOR 8M', 'nor8m'],
     ['NOR 16M', 'nor16m'],
     ['NOR 32M', 'nor32m'],
     ['NAND', 'nand']].freeze
  end

  def list_of_sd_card_for_select
    [['Camera does not have an SD card slot', 'nosd'],
     ['Camera has an SD card slot', 'sd']].freeze
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
