# frozen_string_literal: true

module ApplicationHelper
  def css_debug
    debug_mode? ? 'debug' : nil
  end

  def debug_mode?
    request.remote_addr.eql?('10.10.255.255')
  end

  def default_image_path
    # asset_path, not a literal /assets/ URL: with assets.compile = false only
    # digested filenames exist in the manifest.
    asset_path('no-signal.webp')
  end

  # Bootstrap has no `alert-alert` or `alert-error`, so the keys Rails ships
  # under have to be mapped. Everything unrecognised stays informational, but
  # warning and success are contextual classes in their own right -- mapping
  # them to info too rendered the wizard's "does not fit an 8MB flash chip" as
  # a calm blue notice.
  FLASH_CLASSES = { 'alert' => 'danger', 'error' => 'danger', 'danger' => 'danger',
                    'warning' => 'warning', 'success' => 'success' }.freeze

  def display_flashes
    html = flash.keys.map do |k|
      css = FLASH_CLASSES.fetch(k.to_s, 'info')
      content_tag 'div', flash.discard(k), class: "mt-4 alert alert-#{css}", role: 'alert'
    end.join("\n")
    return if html.blank?

    content_tag 'div', raw(html), class: 'alerts'
  end

  def ipaddr_pattern
    '^((\d{1,2}|1\d\d|2[0-4]\d|25[0-5])\.){3}(\d{1,2}|1\d\d|2[0-4]\d|25[0-5])$'
  end

  def macaddr_pattern
    '^([a-fA-F\d]{2}[:\-]){5}[a-fA-F\d]{2}$'
  end

  # The board rule lives on Soc, which reads it from the name upstream
  # publishes. This used to carry its own copy -- model.downcase with t31 and
  # t40 collapsed -- so the tarball this linked to and the one the server
  # assembled from could disagree: for T23N it offered openipc.t23n-*.tgz,
  # which 404s, and for AK3916EV301 it offered ak3916ev301 while the server
  # built from ak3918ev200. It also contradicted the member names in the
  # SD-card instructions, which come from Soc#kernel_file and Soc#rootfs_file.
  def firmware_filename(camera)
    "openipc.#{camera.soc.board}-#{camera.flash_type_type}-#{camera.firmware_version}.tgz"
  end

  def firmware_url(camera)
    filename = firmware_filename(camera)
    "https://github.com/OpenIPC/firmware/releases/download/latest/#{filename}"
  end

  def link_to_github_profile(username)
    link_to "@#{username}", "https://github.com/#{username}/", class: 'github'
  end

  def link_to_telegram_profile(username)
    link_to "@#{username}", "https://t.me/#{username}", class: 'telegram'
  end

  def link_to_telegram_webchat(username)
    link_to "@#{username}", "https://web.telegram.org/k/@#{username}", class: 'telegram'
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
    content_tag 'p', 'This part is currently under development. Stay tuned.', class: 'alert alert-warning'
  end
end
