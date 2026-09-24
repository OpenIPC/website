# frozen_string_literal: true

# The only way a camera frame leaves this application.
#
# WHY THIS EXISTS. Until 2026-09-23 a frame was a static file nginx handed to
# anyone who asked. That is why four rounds of hardening failed: opaque ids
# (#235) were re-armed off the pages within a day, a JavaScript gate (#261) was
# walked around through the plain link left beside it, an address block (#262)
# was answered by moving off those addresses inside 24 hours, and a rate limit
# never reached a fleet that is distributed by design. Every one of them made
# the frames more EXPENSIVE to take. None of them made the frames ABSENT, and a
# distributed adversary simply paid.
#
# WHAT ACTUALLY STOPS THE HARVEST is that there is no longer an address to
# fetch. A crawler that collects URLs -- which is all of them, including every
# AI crawler measured here -- gets nothing, permanently, without an arms race.
#
# WHAT THE BUDGET BELOW IS AND IS NOT. It is not what stops a distributed
# harvester; the per-address rate limit of #262 taught that lesson, since a
# fleet spread over seven hundred addresses sits under any per-address
# threshold by construction. What it does is stop ONE client taking everything
# in a sitting, and -- through the log line in `unsubscribed` -- make bulk
# collection visible, which a static file never could. Those are worth having.
# Overstating them is not.
#
# NOT A SECURITY CONTROL, and the mask is not encryption. Anything drawn on a
# screen can be photographed, and a headless browser pointed at this site
# specifically could read the canvas back.
class WallChannel < ApplicationCable::Channel
  # Frames one ADDRESS may take per hour.
  #
  # Per address and time-boxed, not per connection and permanent. The first
  # version was the latter and was wrong twice over: a reader browsing several
  # camera-days in one visit exhausted it and silently stopped seeing pictures
  # -- the one failure this work must not cause -- while a harvester simply
  # reconnected for a fresh allowance, which nginx's per-address `limit_conn`
  # does not prevent because it bounds simultaneous sockets, not sequential
  # ones.
  #
  # Sized well clear of any reader. A thorough visit -- the gallery (18), a
  # camera page (13), its whole archive (96) and the slideshow (96) -- is about
  # 223 frames. Carrier-grade NAT pools many readers behind one address and
  # this site has a large audience behind exactly that, so the ceiling sits an
  # order of magnitude above a single visit rather than tuned close to it.
  FRAME_BUDGET = 3_000
  BUDGET_WINDOW = 1.hour

  # Frames per single request. A page asks for everything it needs at once, and
  # nothing legitimate needs more in one message than the longest day a camera
  # can produce.
  MAX_PER_REQUEST = 96

  # How much of a frame the mask covers.
  #
  # It used to cover all of it, and the cost was real: the XOR runs byte by
  # byte in Ruby, so a 250 KB fullhd frame is a quarter of a million
  # iterations and the 83-frame slideshow was twenty million. Measured on
  # production, that page took about thirty seconds to fill -- 15 frames
  # painted at 5 s, 61 at 15 s, 83 at 30 s.
  #
  # The head is enough for what the mask is actually for. A JPEG's SOI marker,
  # APP0 header and quantisation tables live in the first few kilobytes;
  # corrupt those and the file will not decode, which is the entire goal --
  # that somebody recording the socket cannot rename the result .jpg. Masking
  # the pixel data after that adds cost and no property, and calling it
  # encryption would be wrong at any length.
  MASK_BYTES = 4_096

  VARIANTS = WallImage::VARIANTS.map(&:to_s).freeze

  def subscribed
    @served = Set.new
    # Start holding nothing, so every path that is not an accepted grant ends
    # in frames being refused. An uninitialised set that meant "unrestricted"
    # would fail open, which on this channel means handing a stranger the wall.
    revoke
    return reject_without_grant unless accept_grant(params[:grant])

    stream_from "wall:#{connection_id}"
  end

  # A later page on the same socket.
  #
  # Turbo Drive swaps the body and keeps the connection, so a reader who moves
  # from the gallery to a camera page needs the new page's grant to reach the
  # channel without tearing the socket down. `reject` is a subscription-time
  # verb and would raise here, so a bad grant mid-session drops the client back
  # to holding none: the socket stays open, and nothing more is served on it.
  def use_grant(data)
    return if accept_grant(data['grant'])

    revoke
    logger.warn("wall: #{client_key} sent an invalid grant mid-session")
    transmit({ error: 'no grant' })
  end

  def unsubscribed
    return if @served.blank?

    # The visible half of the mechanism. A reader is tens of frames; a
    # collector is hundreds, and says so here whether or not it ever reaches
    # the ceiling.
    logger.info("wall: connection served #{@served.size} distinct frames")
  end

  # `data` is untrusted: it arrives from the page. Everything below treats it
  # as a request for ids that may not exist, in a variant that may not be real,
  # in quantities nobody sane would ask for.
  def request_frames(data)
    variant = data['variant'].to_s
    return reject_request('unknown variant') unless VARIANTS.include?(variant)

    return reject_request('variant not granted') unless granted_variant?(variant)

    ids = Array(data['ids']).map(&:to_s).grep(Snapshot::PUBLIC_ID_FORMAT).uniq
    return reject_request('too many frames in one request') if ids.size > MAX_PER_REQUEST

    # The intersection is the whole mechanism. A client may only ever receive
    # frames some page already showed it, so it cannot walk ids it was not
    # given -- which is what the budget could never prevent, only meter.
    ids.select! { |id| granted_id?(id) }

    ids.each { |id| deliver(id, variant) }
  end

  private

  # Frames a single socket may hold permission for at once.
  #
  # Grants accumulate rather than replace, and that is a deliberate reversal of
  # the first draft. Replacing looks tidier and breaks readers: a page sends
  # its frames in chunks 250 ms apart, and the lazy archive turbo-frame lands
  # in the middle of that with a grant of its own -- so the outer page's
  # remaining chunks would be refused and a reader would watch half a gallery
  # fail to paint. That is the one failure this work must not cause.
  #
  # Accumulating costs little. Retention was never the defence: a harvester
  # that wanted a wider set would open a second socket, which is free. The
  # defence is that a grant only ever names frames a page actually rendered,
  # and that holds however many grants are stacked. The cap is here so a very
  # long session cannot grow without bound, not as a security boundary.
  GRANT_RETENTION = 1_024

  # Accepts a grant, adding what it allows to what this socket already holds.
  def accept_grant(token)
    if grants_disabled?
      @unrestricted = true
      return true
    end

    granted = WallGrant.verify(token)
    return false if granted.nil?

    @granted_ids = Set.new if @granted_ids.size >= GRANT_RETENTION
    @granted_ids.merge(granted[:ids])
    @granted_variants.merge(granted[:variants])
    true
  end

  def revoke
    @unrestricted = false
    @granted_ids = Set.new
    @granted_variants = Set.new
  end

  def granted_id?(id)
    @unrestricted || @granted_ids.include?(id)
  end

  def granted_variant?(variant)
    @unrestricted || @granted_variants.include?(variant)
  end

  # The escape hatch, and the reason it exists.
  #
  # If a grant is ever wrong the wall goes blank for everyone, which this
  # file's own header calls the one failure this work must not cause. Flipping
  # an environment variable on the host restores frames in the time it takes to
  # restart a container, without a rollback and without a deploy. It is a
  # switch for an emergency, not a configuration: when it is on, anybody may
  # take frames again.
  def grants_disabled?
    ENV['WALL_GRANTS_DISABLED'] == '1'
  end

  def reject_without_grant
    # Logged at warn and counted, because this is now the number that says
    # whether the harvest has stopped: before this change 94% of the clients
    # on the channel had never loaded a page.
    logger.warn("wall: #{client_key} subscribed without a valid grant")
    transmit({ error: 'no grant' })
    reject
  end

  def deliver(id, variant)
    return if over_budget?

    bytes = read_frame(id, variant)
    # A frame with no file is a purged snapshot or an id that never existed,
    # and those look identical from here on purpose.
    return if bytes.nil?

    @served << id
    transmit_frame(id, variant, bytes)
  end

  # File.exist? followed by File.binread is a race: PurgeImagesJob destroys
  # snapshots on a nightly cron and Snapshot#purge_wall_images removes the
  # whole directory, so a frame can vanish between the two calls and take the
  # channel action down with an ENOENT. Ask for the bytes and accept that they
  # may not be there.
  def read_frame(id, variant)
    File.binread(WallImage.path_for(id, variant))
  rescue Errno::ENOENT, Errno::EACCES
    nil
  end

  def over_budget?
    return false if spent < FRAME_BUDGET

    unless @refused
      @refused = true
      logger.warn("wall: #{client_key} refused at #{spent} frames in the hour")
      transmit({ error: 'frame budget reached' })
    end
    true
  end

  def spent
    Rails.cache.read(budget_key).to_i
  end

  def charge
    Rails.cache.increment(budget_key, 1, expires_in: BUDGET_WINDOW) ||
      Rails.cache.write(budget_key, 1, expires_in: BUDGET_WINDOW)
  end

  # Bucketed by window so the count ages out wholesale rather than needing a
  # sliding structure. Keyed on the reader's address: set_real_ip_from in
  # nginx.conf has already turned a mirror's forwarded address back into the
  # reader before this sees it.
  def budget_key
    @budget_key ||= "wall:frames:#{client_key}:#{Time.now.to_i / BUDGET_WINDOW}"
  end

  # Supplied by the connection rather than read from the request here:
  # ActionCable::Connection::Base#request is private, so reaching for it from a
  # channel raises on every message. See the note in application_cable.
  def client_key
    client_ip.presence || 'unknown'
  end

  # The mask is obfuscation and nothing more. It costs a scraper a look at this
  # file; it costs a reader nothing. Its only real job is to stop the frames
  # being recoverable by pointing a recorder at the socket and renaming the
  # result .jpg. Do not describe it as encryption anywhere.
  #
  # Braces are load-bearing on transmit: ActionCable's takes a positional hash,
  # and `transmit(id: ...)` in Ruby 3 passes keywords instead, which is an
  # ArgumentError at the first frame.
  def transmit_frame(id, variant, bytes)
    charge

    # The VARIANT travels back with the frame, and must. The client keys its
    # waiting canvases on id and variant together, because one snapshot appears
    # on a page twice at different sizes -- the fullhd hero and the first icon2
    # tile of its own archive strip are the same id. Answering with the id
    # alone let whichever response arrived first paint both canvases, so the
    # hero could be drawn from a 240x135 thumbnail.
    transmit({ id: id, variant: variant, connection_id: connection_id,
               frame: Base64.strict_encode64(mask(bytes)) })
  end

  # Only the first MASK_BYTES are touched; the rest is copied through. The
  # client mirrors this exactly, so the two constants have to agree -- a test
  # asserts the round trip rather than trusting that they do.
  def mask(bytes)
    key = mask_key
    head = bytes.byteslice(0, MASK_BYTES)
    masked = head.bytes.each_with_index.map { |b, i| b ^ key[i % key.size] }.pack('C*')

    masked + bytes.byteslice(MASK_BYTES..).to_s
  end

  def mask_key
    @mask_key ||= connection_id.each_char.map(&:ord)
  end

  def reject_request(reason)
    logger.warn("wall: refused request -- #{reason}")
    transmit({ error: reason })
  end
end
