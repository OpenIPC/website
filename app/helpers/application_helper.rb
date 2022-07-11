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

  def partition_names
    %w[boot env kernel rootfs rootfs_data]
  end
  def partition_sizes
    %w[256 64 2048 5120 -]
  end
end
