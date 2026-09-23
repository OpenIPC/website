# frozen_string_literal: true

require 'test_helper'
require 'minitest/mock'

# The camera upload API.
#
# This endpoint had no test at all until 2026-09. It is unauthenticated and
# CSRF is skipped on it, and the cameras POSTing to it run firmware in the
# field that cannot be upgraded -- so its observable contract is frozen whether
# we like it or not, and anything that changes it breaks devices nobody can
# reach.
#
# The fleet is smaller than the request count suggests: 1,281 uploads in the 24
# hours to 2026-09-19, from 16 distinct MAC addresses. Few devices, each on a
# quarter-hour cron, which is also why the load arrives in bursts on the hour.
#
# Several changes are queued up that sit in front of or underneath it: real
# client addresses from the mirrors, per-IP rate limits, a different path for
# the images it stores. Every one of them can break this invisibly -- by
# buffering the body, rewriting Content-Type, dropping the X-Error header on a
# rejection, dropping the computed Retry-After, or collapsing request.remote_ip
# so the whitelist stops matching. These tests exist to make that loud.
#
# They pin *current* behaviour, including the parts that look like defects.
# Where something is arguably wrong the test says so and still asserts what the
# endpoint does today, because cameras depend on today's behaviour.
class SnapshotsControllerTest < ActionDispatch::IntegrationTest
  # A JPEG comment segment carries a 16-bit length that counts itself, so one
  # segment holds at most 65_533 bytes of payload.
  MAX_COMMENT_PAYLOAD = 65_533
  JPEG_HEAD = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00".b
  JPEG_TAIL = "\xFF\xD9".b

  # A real-shaped JPEG of an exact size: SOI, a JFIF APP0, enough comment
  # segments to reach the requested length, then EOI.
  #
  # Generated rather than committed as a fixture because several of these tests
  # are about the validator's size bounds, and because the suite should not
  # carry binary blobs. The size has to come out exact for the same reason.
  #
  # The padding is split across segments rather than declared in one: a single
  # 16-bit length cannot describe more than 64 KB, and writing the full figure
  # into it does not raise -- pack('n') truncates to the low bits, so a 5 MB
  # file would quietly claim a 1 KB comment and stop being a valid JPEG.
  # Nothing here decodes it, but a helper that lies about its own output is a
  # bad thing to leave in a test.
  def jpeg_bytes(size)
    budget = size - JPEG_HEAD.bytesize - JPEG_TAIL.bytesize
    raise ArgumentError, "#{size} is too small to build a JPEG from" if budget < 5

    JPEG_HEAD + comment_payloads(budget).map { |bytes| comment_segment(bytes) }.join.b + JPEG_TAIL
  end

  def comment_segment(payload)
    "\xFF\xFE".b + [payload + 2].pack('n') + ("\x20".b * payload)
  end

  # Payload sizes for the comment segments filling `budget` bytes exactly,
  # counting the four bytes of marker and length each one costs. Spread evenly
  # so no segment ends up too small to be worth emitting.
  def comment_payloads(budget)
    count = (budget.to_f / (MAX_COMMENT_PAYLOAD + 4)).ceil
    base, extra = (budget - (4 * count)).divmod(count)
    Array.new(count) { |i| base + (i < extra ? 1 : 0) }
  end

  # An ISO-BMFF ftyp box declaring the heic brand. Marcel identifies HEIF from
  # these bytes alone, which is all this endpoint looks at -- decoding one
  # needs libvips built with libheif, and that happens later, in the variant
  # job, not on the request.
  def heif_bytes(size)
    ftyp = [20].pack('N') + 'ftyp'.b + 'heic'.b + [0].pack('N') + 'heic'.b
    ftyp + ("\x00".b * [size - ftyp.bytesize, 0].max)
  end

  def upload(data, content_type = 'image/jpeg', filename = 'snapshot.jpg')
    file = Tempfile.new(['snapshot', File.extname(filename)])
    file.binmode
    file.write(data)
    file.rewind
    (@tempfiles ||= []) << file
    Rack::Test::UploadedFile.new(file.path, content_type, original_filename: filename)
  end

  teardown do
    Array(@tempfiles).each(&:close!)
  end

  def post_snapshot(file: nil, mac: '00:11:22:33:44:55', remote_addr: '203.0.113.9', **extra)
    file ||= upload(jpeg_bytes(12.kilobytes))
    post '/snapshots',
         params: { file:, mac_address: mac, soc: 'gk7205v300', sensor: 'imx307',
                   firmware: 'lite', streamer: 'majestic' }.merge(extra),
         headers: { 'REMOTE_ADDR' => remote_addr }
  end

  # The model reads both lists through credentials.dig, which answers nil
  # without config/master.key -- which is exactly what the test environment
  # has. Stubbing dig rather than the reader keeps that nil-tolerance intact
  # for every key this does not name.
  #
  # minitest/mock is required at the top of this file for the same reason every
  # other test here requires it: rails/test_help does not load it, so Object#stub
  # does not exist by default. That matters more than usual on this object.
  # EncryptedConfiguration forwards unknown methods to the decrypted config, so
  # without the require `credentials.stub(...)` does not raise NoMethodError --
  # it resolves to the delegation, returns nil, and never runs the block. The
  # tests then fail somewhere else entirely, on a nil @response.
  def with_credentials(mac_blacklisted: [], ip_whitelisted: [], &block)
    Rails.application.credentials.stub(:dig, lambda { |*keys|
      return mac_blacklisted if keys == %i[mac blacklisted]
      return ip_whitelisted  if keys == %i[ip whitelisted]

      nil
    }, &block)
  end

  # The size bound tests are only meaningful if this comes out exact, and the
  # multi-segment path only starts above 64 KB.
  test 'the generated JPEG is exactly the size asked for' do
    [10.kilobytes, 12.kilobytes, 64.kilobytes + 1, 1.megabyte, 5.megabytes + 1.kilobyte].each do |size|
      data = jpeg_bytes(size)

      assert_equal size, data.bytesize, "jpeg_bytes(#{size}) produced #{data.bytesize} bytes"
      assert data.start_with?("\xFF\xD8".b), 'must start with the JPEG SOI marker'
      assert data.end_with?("\xFF\xD9".b), 'must end with the JPEG EOI marker'
    end
  end

  # --- accepted uploads ---------------------------------------------------

  test 'a JPEG upload is accepted and answers 201 with the new location' do
    assert_difference 'Snapshot.count', 1 do
      post_snapshot
    end

    assert_response :created
    assert_equal snapshot_path(id: Snapshot.last), response.headers['Location']
  end

  # HEIF is why every variant is rendered to JPEG: the cameras can send it and
  # only Safari can display it. The upload path must take it even though
  # nothing decodes it until the variant job runs.
  test 'a HEIF upload is accepted' do
    assert_difference 'Snapshot.count', 1 do
      post_snapshot(file: upload(heif_bytes(12.kilobytes), 'image/heic', 'snapshot.heic'))
    end

    assert_response :created
    assert_equal 'image/heic', Snapshot.last.file.blob.content_type
  end

  # soc and sensor are whatever the camera chose to send, and both are
  # nullable. The homepage builds its tile caption from them, and an upload
  # with neither once took the whole page down -- see the reject(&:blank?) in
  # pages/home.html.erb.
  test 'an upload carrying neither soc nor sensor is accepted' do
    assert_difference 'Snapshot.count', 1 do
      post_snapshot(soc: '', sensor: '')
    end

    assert_response :created
  end

  test 'the uploading address is recorded from the request, not from a parameter' do
    post_snapshot(remote_addr: '198.51.100.7', ip_address: '10.0.0.1')

    assert_response :created
    assert_equal '198.51.100.7', Snapshot.last.ip_address
  end

  # --- refused uploads ----------------------------------------------------

  # 415 with the reason in a header, not a body: `head` sends no body at all,
  # and X-Error is the only thing a camera has to log.
  test 'a non-image is refused with 415 and says why in X-Error' do
    assert_no_difference 'Snapshot.count' do
      post_snapshot(file: upload('not an image ' * 1000, 'text/plain', 'readme.txt'))
    end

    assert_response :unsupported_media_type
    assert_match(/not a valid file format/i, response.headers['X-Error'].to_s)
  end

  test 'a file under the 10 KB floor is refused' do
    assert_no_difference 'Snapshot.count' do
      post_snapshot(file: upload(jpeg_bytes(2.kilobytes)))
    end

    assert_response :unsupported_media_type
    assert_match(/size/i, response.headers['X-Error'].to_s)
  end

  # Worth knowing when reading this: in production the 5 MB ceiling is
  # unreachable. The origin sets no client_max_body_size, so nginx's 1 MB
  # default rejects anything larger with 413 before Rails is reached -- on
  # 2026-09-18 that was 11 uploads, and the largest blob ever stored is
  # 1,000,919 bytes. The model's own limit is still what this asserts, because
  # that is the contract the app owns.
  test 'a file over the 5 MB ceiling is refused' do
    assert_no_difference 'Snapshot.count' do
      post_snapshot(file: upload(jpeg_bytes(5.megabytes + 1.kilobyte)))
    end

    assert_response :unsupported_media_type
    assert_match(/size/i, response.headers['X-Error'].to_s)
  end

  test 'an upload with no file at all is refused' do
    assert_no_difference 'Snapshot.count' do
      post '/snapshots', params: { mac_address: '00:11:22:33:44:55' }
    end

    assert_response :unsupported_media_type
  end

  test 'an upload with no MAC address is refused' do
    assert_no_difference 'Snapshot.count' do
      post_snapshot(mac: '')
    end

    assert_response :unsupported_media_type
  end

  test 'a malformed MAC address is refused' do
    assert_no_difference 'Snapshot.count' do
      post_snapshot(mac: 'not-a-mac')
    end

    assert_response :unsupported_media_type
  end

  # Content type is whatever the client declares, unless the bytes say
  # otherwise decisively. Marcel reads the magic first, but treats text and
  # unrecognised binary as non-committal and falls back to the declared type --
  # so a camera can store arbitrary bytes here by labelling them image/jpeg.
  #
  # Pinned rather than fixed: the endpoint has always behaved this way and the
  # fleet cannot be updated. It matters for anything that later serves these
  # blobs straight from disk, which is what #146 does -- a stored blob is not
  # guaranteed to be an image.
  test 'the declared content type is trusted when the bytes are not decisive' do
    assert_difference 'Snapshot.count', 1 do
      post_snapshot(file: upload('not an image ' * 1000, 'image/jpeg', 'snapshot.jpg'))
    end

    assert_response :created
    assert_equal 'image/jpeg', Snapshot.last.file.blob.content_type
  end

  # --- blacklist ----------------------------------------------------------

  # 403 comes from an exception raised inside a validation, not from a
  # validation failure, so it bypasses the 415 path entirely and sends no
  # X-Error. A camera on this list gets a bare refusal.
  test 'a blacklisted MAC is refused with 403' do
    with_credentials(mac_blacklisted: ['de:ad:be:ef:00:01']) do
      assert_no_difference 'Snapshot.count' do
        post_snapshot(mac: 'de:ad:be:ef:00:01')
      end
    end

    assert_response :forbidden
  end

  test 'a MAC that is not on the blacklist is unaffected by it' do
    with_credentials(mac_blacklisted: ['de:ad:be:ef:00:01']) do
      post_snapshot(mac: '00:11:22:33:44:66')
    end

    assert_response :created
  end

  # --- the 15 minute interval --------------------------------------------

  test 'a second upload from the same camera inside the interval is refused with 429' do
    post_snapshot(mac: '00:11:22:33:44:77')
    assert_response :created

    assert_no_difference 'Snapshot.count' do
      post_snapshot(mac: '00:11:22:33:44:77')
    end

    assert_response :too_many_requests
  end

  # The camera has nothing else to go on, so this header is the whole of the
  # backoff protocol.
  test 'the 429 carries a Retry-After the camera can wait on' do
    post_snapshot(mac: '00:11:22:33:44:88')
    post_snapshot(mac: '00:11:22:33:44:88')

    assert_response :too_many_requests
    retry_after = response.headers['Retry-After'].to_i
    assert retry_after.positive?, 'Retry-After must be a positive number of seconds'
    assert retry_after <= Snapshot::INTERVAL_LIMIT.to_i,
           'Retry-After must not exceed the interval itself'
  end

  # Two minutes of hysteresis: the gate opens at 13 minutes even though the
  # limit is called 15, so a camera on a 15 minute cron is not refused for
  # arriving a little early.
  test 'the interval opens two minutes early, by design' do
    post_snapshot(mac: '00:11:22:33:44:99')
    Snapshot.last.update_column(:created_at, 13.minutes.ago - 1.second)

    assert_difference 'Snapshot.count', 1 do
      post_snapshot(mac: '00:11:22:33:44:99')
    end

    assert_response :created
  end

  test 'a camera arriving twelve minutes later is still refused' do
    post_snapshot(mac: '00:11:22:33:44:aa')
    Snapshot.last.update_column(:created_at, 12.minutes.ago)

    post_snapshot(mac: '00:11:22:33:44:aa')

    assert_response :too_many_requests
  end

  # The whitelist is keyed on the address the request arrived from. If anything
  # in front of the app collapses remote_ip -- a proxy that does not forward
  # it, an edge that terminates the connection -- this stops matching and the
  # exempted uploader starts getting 429s.
  test 'a whitelisted address is exempt from the interval' do
    with_credentials(ip_whitelisted: ['198.51.100.42']) do
      post_snapshot(mac: '00:11:22:33:44:bb', remote_addr: '198.51.100.42')
      assert_response :created

      assert_difference 'Snapshot.count', 1 do
        post_snapshot(mac: '00:11:22:33:44:bb', remote_addr: '198.51.100.42')
      end
    end

    assert_response :created
  end

  test 'the exemption is for the address, not the camera' do
    with_credentials(ip_whitelisted: ['198.51.100.42']) do
      post_snapshot(mac: '00:11:22:33:44:cc', remote_addr: '198.51.100.42')
      assert_response :created

      post_snapshot(mac: '00:11:22:33:44:cc', remote_addr: '203.0.113.9')
    end

    assert_response :too_many_requests
  end

  # Different cameras do not queue behind one another.
  test 'the interval is per camera' do
    post_snapshot(mac: '00:11:22:33:44:dd')
    assert_response :created

    assert_difference 'Snapshot.count', 1 do
      post_snapshot(mac: '00:11:22:33:44:ee')
    end

    assert_response :created
  end

  # --- the endpoint's own shape -------------------------------------------

  # Cameras POST straight to this URL with no session and no token. If
  # protect_from_forgery is ever applied here the entire fleet stops uploading
  # at once, which is a thing to find in CI rather than in the wall going
  # quiet.
  test 'the upload needs no CSRF token' do
    original = ActionController::Base.allow_forgery_protection
    ActionController::Base.allow_forgery_protection = true

    post_snapshot(mac: '00:11:22:33:44:ff')

    assert_response :created
  ensure
    ActionController::Base.allow_forgery_protection = original
  end
end
