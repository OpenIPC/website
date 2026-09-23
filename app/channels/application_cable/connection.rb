# frozen_string_literal: true

module ApplicationCable
  # No identity. The Open Wall is public and there is no user model on this
  # site, so a connection is anonymous by design; what it is NOT is
  # unaccountable. `WallChannel` counts what each one takes and refuses it past
  # a budget, which is the whole reason frames moved off static files.
  #
  # `remote_ip` is kept for the log line only. set_real_ip_from in nginx.conf
  # rewrites it to the reader rather than the mirror before this sees it.
  class Connection < ActionCable::Connection::Base
    identified_by :connection_id

    def connect
      self.connection_id = SecureRandom.hex(8)
      logger.add_tags('WallCable', connection_id, request.remote_ip)
    end
  end
end
