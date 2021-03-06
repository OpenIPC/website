module ApplicationHelper
  def debug_mode?
    request.remote_addr.eql?("127.0.0.1")
  end

  def css_debug
    debug_mode? ? "debug" : nil
  end

  def ipaddr_pattern
    '^((\d{1,2}|1\d\d|2[0-4]\d|25[0-5])\.){3}(\d{1,2}|1\d\d|2[0-4]\d|25[0-5])$'
  end

  def link_to_github_profile(username)
    link_to "@#{username}", "https://github.com/#{username}/", class: "github"
  end

  def link_to_telegram_profile(username)
    link_to "@#{username}", "https://t.me/#{username}", class: "telegram"
  end

  def link_to_telegram_webchat(username)
    link_to "@#{username}", "https://web.telegram.org/k/@#{username}", class: "telegram"
  end

  def partition_names
    %w[boot env kernel rootfs rootfs_data]
  end

  def partition_sizes
    %w[256 64 2048 5120 -]
  end

  def mtd_names
    %w[hi_sfc hinand jz_sfc nor-flash NOR_FLASH sfc spi0.0 spi_flash xm_sfc]
  end


  def under_development
    content_tag "p", "This part is currently under development. Stay tuned.", class: "alert alert-warning"
  end
end
