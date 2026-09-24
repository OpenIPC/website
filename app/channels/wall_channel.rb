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
  # Sized against BOTH the reader and the corpus, which the first version was
  # not.
  #
  # That version reasoned only from a reader: a thorough visit -- the gallery
  # (18), a camera page (13), its whole archive (96) and the slideshow (96) --
  # is about 223 frames, so it put the ceiling an order of magnitude above at
  # 3,000 and called that safe. It never asked how large the thing being
  # protected is. The wall holds about 2,767 frames across 20 cameras, so the
  # budget was LARGER THAN THE ENTIRE WALL: one address, fetching pages for
  # grants, could take every frame that exists inside an hour and never come
  # near the limit.
  #
  # 1,000 is a compromise and worth naming as one rather than presenting as a
  # calculation. It keeps the four-fold margin over a thorough visit that the
  # carrier-NAT case needs -- several readers do share one address here, and a
  # reader who stops seeing pictures is the failure this work must not cause --
  # while cutting a single address from "the entire wall in an hour" to about a
  # third of it, so a full sweep costs three hours instead of one.
  #
  # Tighter was tried and put back. At 500 the margin over a thorough visit
  # falls to roughly two, and three people behind one carrier-grade address
  # browsing properly in the same hour would start being refused. With a corpus
  # this small -- 2,767 frames against a 223-frame visit -- there is no
  # per-address number that both guards the wall and is safe for a shared
  # address, and pretending otherwise would be the same error as the 3,000.
  #
  # Which is the honest summary of this constant: it bounds what ONE address
  # can take and nothing more. It is not what stops a distributed fleet, it
  # never was, and the page grant is what does that now.
  FRAME_BUDGET = 1_000
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
    logger.warn("wall_grant_refused #{client_key} sent an invalid grant mid-session")
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

    ids = Array(data['ids']).map(&:to_s).grep(Snapshot::PUBLIC_ID_FORMAT).uniq
    return reject_request('too many frames in one request') if ids.size > MAX_PER_REQUEST

    # The intersection is the whole mechanism, and it is over PAIRS rather than
    # over ids and variants separately. A client may only ever receive a frame
    # at a size some page actually drew it at: checking the two independently
    # let the fullhd permission from a snapshot page's hero be spent on the
    # icon2 ids of its archive strip, which returns a full-resolution view of a
    # camera the reader had only been shown as a thumbnail.
    ids.select! { |id| granted?(id, variant) }

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

    @granted = Set.new if @granted.size >= GRANT_RETENTION
    @granted.merge(granted)
    true
  end

  def revoke
    @unrestricted = false
    @granted = Set.new
  end

  def granted?(id, variant)
    @unrestricted || @granted.include?(WallGrant.pair(id, variant))
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
    # `wall_grant_refused` is a marker, not prose: deploy/log-report.sh counts
    # it to produce the bare-socket figure, which is the number this whole
    # change is judged on. Do not reword it without changing the report.
    logger.warn("wall_grant_refused #{client_key} subscribed without a valid grant")
    transmit({ error: 'no grant' })
    reject
  end

  # Take the budget FIRST, then decide.
  #
  # This used to read the counter, and charge later inside transmit_frame. Two
  # operations, and nothing between them: several sockets on one address could
  # each read a figure under the ceiling and then all deliver. nginx allows
  # sixteen concurrent sockets per address and a request may name ninety-six
  # frames, so the overshoot was not theoretical -- an address could take well
  # over the limit by opening connections in parallel, which is precisely what
  # a harvester does and exactly the hole a tightened ceiling is supposed to
  # close.
  #
  # `Rails.cache.increment` is atomic, so reserving before reading collapses
  # admission and charging into one operation. A reservation that turns out to
  # be unspendable -- the frame was purged between the grant and the read -- is
  # given back, because a reader must not be charged for a picture they never
  # received.
  def deliver(id, variant)
    return unless reserve

    bytes = read_frame(id, variant)
    # A frame with no file is a purged snapshot or an id that never existed,
    # and those look identical from here on purpose.
    if bytes.nil?
      refund
      return
    end

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

  # Claim one frame's worth of budget, or refuse.
  def reserve
    taken = charge
    return true if taken <= FRAME_BUDGET

    # Hand back the unit this call just took, so a client sitting against the
    # ceiling cannot inflate the counter for everyone else behind the same
    # carrier-grade address simply by continuing to ask.
    refund

    unless @refused
      @refused = true
      logger.warn("wall: #{client_key} refused at #{FRAME_BUDGET} frames in the hour")
      transmit({ error: 'frame budget reached' })
    end
    false
  end

  # What this address has spent across the window, not just the current bucket.
  #
  # The counter lives in fixed clock-hour buckets, which is cheap and ages out
  # wholesale -- but taken alone it is not an hourly limit at all: an address
  # that spends its whole allowance at 10:59 gets a fresh one at 11:00, so the
  # real worst case was twice the stated figure across two minutes. The PR that
  # tightened this constant claimed an hourly bound it did not have.
  #
  # The previous bucket is therefore counted too, weighted by how much of it
  # still falls inside the trailing hour: at 11:15 the 10:00 bucket is 75%
  # relevant. This is the standard sliding-window approximation and it costs one
  # extra cache read. It is an approximation -- a burst concentrated at the very
  # start of the previous bucket is over-counted -- and that error is in the
  # direction of refusing slightly early rather than serving twice over, which
  # is the right way round for a ceiling.
  def spent
    current = Rails.cache.read(budget_key).to_i
    previous = Rails.cache.read(previous_budget_key).to_i
    current + (previous * (1.0 - window_elapsed)).round
  end

  # How far through the current bucket we are, 0.0 to 1.0.
  def window_elapsed
    (Time.now.to_i % BUDGET_WINDOW.to_i) / BUDGET_WINDOW.to_f
  end

  def refund
    Rails.cache.decrement(budget_key, 1)
  end

  # Returns what this address has now spent across the window, including the
  # unit just taken. The increment is atomic; the carried-over part of the
  # previous bucket is added afterwards because it never changes under us.
  def charge
    taken = Rails.cache.increment(budget_key, 1, expires_in: BUDGET_WINDOW)
    taken ||= (Rails.cache.write(budget_key, 1, expires_in: BUDGET_WINDOW) && 1)

    previous = Rails.cache.read(previous_budget_key).to_i
    taken + (previous * (1.0 - window_elapsed)).round
  end

  # Bucketed by window so the count ages out wholesale rather than needing a
  # sliding structure. Keyed on the reader's address: set_real_ip_from in
  # nginx.conf has already turned a mirror's forwarded address back into the
  # reader before this sees it.
  # Not memoised: a long-lived socket outlives its bucket, and a cached key
  # would keep charging an hour that has ended.
  def budget_key
    "wall:frames:#{client_key}:#{bucket}"
  end

  def previous_budget_key
    "wall:frames:#{client_key}:#{bucket - 1}"
  end

  def bucket
    Time.now.to_i / BUDGET_WINDOW.to_i
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
