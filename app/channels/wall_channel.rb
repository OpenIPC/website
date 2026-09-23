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
# What a static file cannot do, and this can, is COUNT. There is no session
# behind a GET for a file, so there was no way to say "this client has taken
# five hundred frames" -- and that sentence is the entire difference. A reader
# looking at a wall of twenty cameras asks for about twenty frames. The
# harvester took roughly 2,800 distinct ones a day. Three orders of magnitude,
# and until now nothing was in a position to notice.
#
# WHAT THIS IS NOT. It is not a security control and the mask below is not
# encryption. Anything drawn on a screen can be photographed, and a headless
# browser pointed at this site specifically could read the canvas back. The
# claim is narrower and worth more: every crawler and bot that collects URLs --
# which is all of them, including every AI crawler measured here -- gets
# nothing, permanently, without an arms race, because there is no URL.
class WallChannel < ApplicationCable::Channel
  # Distinct frames one connection may have before it is refused.
  #
  # Sized from measurement, not taste. The gallery renders 18 tiles
  # (SnapshotsController::PER_PAGE), a snapshot page 13 (hero + STRIP_EAGER),
  # and the archive view of a camera at the 15-minute upload interval is 96.
  # So a reader who opens the wall, a camera, and that camera's whole day is
  # around 130. Beyond that nobody is reading; they are collecting.
  #
  # Deliberately generous. The cost of being slightly too high is that a
  # harvester gets a few more frames per connection than it needs; the cost of
  # being too low is a reader hitting a wall mid-page, which is the failure
  # this whole line of work must not cause.
  FRAME_BUDGET = 150

  # Frames per single request. A page asks for everything it needs at once, and
  # nothing legitimate needs more in one message than the longest day a camera
  # can produce.
  MAX_PER_REQUEST = 96

  VARIANTS = WallImage::VARIANTS.map(&:to_s).freeze

  def subscribed
    @served = Set.new
    stream_from "wall:#{connection_id}"
  end

  def unsubscribed
    return if @served.blank?

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

    ids.each { |id| deliver(id, variant) }
  end

  private

  def deliver(id, variant)
    return if over_budget?

    path = WallImage.path_for(id, variant)
    # A frame whose file is missing is not an error worth telling a client
    # about in detail -- it is a purged snapshot, or an id that never existed,
    # and those look identical from here on purpose.
    return unless File.exist?(path)

    @served << id
    transmit_frame(id, File.binread(path))
  end

  def over_budget?
    return false if @served.size < FRAME_BUDGET

    unless @refused
      @refused = true
      logger.warn("wall: connection refused at #{@served.size} distinct frames")
      transmit({ error: 'frame budget reached' })
    end
    true
  end

  # Braces are load-bearing: ActionCable's transmit takes a positional hash,
  # and `transmit(id: ...)` in Ruby 3 passes keywords instead, which is an
  # ArgumentError at the first frame.
  #
  # The mask is obfuscation and nothing more. It costs a scraper a look at this
  # file; it costs a reader nothing. Its only real job is to stop the frames
  # being recoverable by pointing a recorder at the socket and renaming the
  # result .jpg. Do not describe it as encryption anywhere.
  def transmit_frame(id, bytes)
    key = mask_key
    masked = bytes.bytes.each_with_index.map { |b, i| b ^ key[i % key.size] }.pack('C*')

    # connection_id travels with the frame because it IS the mask key, and a
    # key sent once on subscribe would be lost the moment Turbo swapped the
    # page and the client re-requested. Sixteen characters per frame.
    transmit({ id: id, connection_id: connection_id, frame: Base64.strict_encode64(masked) })
  end

  def mask_key
    @mask_key ||= connection_id.each_char.map(&:ord)
  end

  def reject_request(reason)
    logger.warn("wall: refused request -- #{reason}")
    transmit({ error: reason })
  end
end
