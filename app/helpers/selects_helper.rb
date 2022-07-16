module SelectsHelper
  def list_of_network_interfaces_for_select
    [['Camera only has Ethernet network', 'eth'],
     ['Camera only has Wireless network', 'wifi'],
     ['Camera has both Ethernet and Wireless networks', 'both']]
  end

  def list_of_flash_type_sizes_for_select
    [['NOR 8M', 'nor8m'],
     ['NOR 16M', 'nor16m'],
     ['NAND', 'nand']]
  end

  def list_of_sd_card_for_select
    [['Camera has an SD card slot', 'sd'],
     ['Camera does not have an SD card slot', 'nosd']]
  end

  def list_of_socs_for_select
    Soc.all.map { |x| [x.full_name, x.id] }
  end

  def list_of_stages_for_select
    Soc::STATUS.map do |key,value|
      ["#{key.upcase}: #{value}", key]
    end
  end
end