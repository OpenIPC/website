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
    # client_ip is identified_by, not read from `request` later, and that is
    # not a style choice. ActionCable::Connection::Base#request is PRIVATE, so
    # a channel calling connection.request raises NoMethodError on every
    # message -- and `stub_connection` in a channel test exposes whatever you
    # hand it as a public attribute, so the suite stays green while production
    # is broken on the first frame. Resolve it here, where request is legal,
    # and pass the value.
    identified_by :connection_id, :client_ip

    def connect
      self.connection_id = SecureRandom.hex(8)
      # set_real_ip_from in nginx.conf has already turned a mirror's forwarded
      # address back into the reader's before this sees it.
      self.client_ip = request.remote_ip.to_s
      logger.add_tags('WallCable', connection_id, client_ip)
    end
  end
end
