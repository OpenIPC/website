module ApplicationHelper
  def debug_mode?
    request.remote_addr.eql?("127.0.0.1")
  end

  def css_debug
    debug_mode? ? "debug" : nil
  end

  def icon_download
    image_tag 'download.svg', alt: 'Image: download'
  end

  def ipaddr_pattern
    '^((\d{1,2}|1\d\d|2[0-4]\d|25[0-5])\.){3}(\d{1,2}|1\d\d|2[0-4]\d|25[0-5])$'
  end

  def flash_type_sizes
    [
      ['NOR 8M', 'nor8m'],
      ['NOR 16M', 'nor16m'],
      ['NAND', 'nand']
    ]
  end

  def link_to_github_profile(username)
    link_to "@#{username}", "https://github.com/#{username}/", class:"github"
  end

  def link_to_telegram_profile(username)
    link_to "@#{username}", "https://t.me/#{username}", class: "telegram"
  end

  def link_to_telegram_webchat(username)
    link_to "@#{username}", "https://web.telegram.org/k/@#{username}", class: "telegram"
  end

  def network_ifaces
    [
      ['Camera only has Ethernet network', 'eth'],
      ['Camera only has Wireless network', 'wifi'],
      ['Camera has both Ethernet and Wireless networks', 'both']
    ]
  end

  def partition_names
    %w[boot env kernel rootfs rootfs_data]
  end

  def partition_sizes
    %w[256 64 2048 5120 -]
  end

  def cd_card
    [
      ['Camera has an SD card slot', 'sd'],
      ['Camera does not have an SD card slot', 'nosd']
    ]
  end
end
