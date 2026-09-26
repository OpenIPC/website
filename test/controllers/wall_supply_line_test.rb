# frozen_string_literal: true

require 'test_helper'

# What a snapshot page hands to whoever fetched it.
#
# #235 made the ids unguessable, which ended the walk of the range. It did not
# end the harvest, because the pages were handing the ids out: a camera at the
# 15-minute INTERVAL_LIMIT has 96 frames in a day, /snapshots/<id> wrote all 96
# into the strip, and /oneday wrote the same 96 as full-HD carousel slides. One
# fetch of any snapshot page was therefore worth 96 more addresses, and the
# Azure fleet re-armed on the opaque ids inside a day without ever guessing
# one.
#
# Measured on the origin over eight hours on 2026-09-22: 7,128 fetches of the
# show page cost 617 MB, the largest single item the site served, against 604
# page views the beacon could see. The fleet took 9,352 pages and 51 images in
# 9,623 requests -- it wants this list, not the pictures, which is why the
# answer is here and not in how the images are delivered.
#
# So these tests are about a count, not about a feature: the number of sibling
# addresses a render gives away must be bounded by STRIP_EAGER and must not
# grow with the camera's day. Every frame stays reachable -- the assertions
# below check that too, because a page that leaks nothing by showing nothing
# would pass the first half of this file and be useless.
class WallSupplyLineTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b
  MAC = '00:11:22:33:44:88'

  # A day of one camera, at the upload interval the rate limit allows. Saved
  # with validate: false for the same reason the opaque-id test does it: the
  # per-MAC interval limit exists to stop exactly this.
  def upload_day(frames:, mac: MAC)
    now = Time.zone.now
    Array.new(frames) do |i|
      snapshot = Snapshot.new(mac_address: mac, ip_address: '203.0.113.9',
                              soc: 'gk7205v300', sensor: 'imx307')
      snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                           content_type: 'image/jpeg')
      snapshot.created_at = now - (i * Snapshot::INTERVAL_LIMIT)
      snapshot.save!(validate: false)
      snapshot
    end
  end

  # Every snapshot address in a response, the breadcrumb's own link included.
  def addresses_in(body)
    body.scan(%r{/snapshots/[0-9a-f]{20}}).uniq
  end

  test 'the strip a snapshot page renders does not grow with the camera day' do
    frames = upload_day(frames: 30)

    get "/snapshots/#{frames.first.public_id}"
    assert_response :success

    icons = response.body.scan('class="card-img"').size
    assert_equal SnapshotsController::STRIP_EAGER, icons,
                 'the page renders a screenful, not the whole day'
  end

  # The assertion the change exists for. 30 frames and 96 frames must cost the
  # fetcher the same number of addresses.
  test 'a bigger day does not hand out more addresses' do
    small = upload_day(frames: 20, mac: '00:11:22:33:44:01')
    get "/snapshots/#{small.first.public_id}"
    few = addresses_in(response.body).size

    big = upload_day(frames: 96, mac: '00:11:22:33:44:02')
    get "/snapshots/#{big.first.public_id}"
    many = addresses_in(response.body).size

    assert_equal few, many, <<~MESSAGE.chomp
      A camera with 96 frames gave away #{many} addresses and one with 20 gave
      away #{few}. That difference is the supply line: it is what let a scraper
      that cannot guess an id collect them anyway, one page at a time.
    MESSAGE
    assert_operator many, :<=, SnapshotsController::STRIP_EAGER + 1,
                    'the strip, plus the breadcrumb link to the page itself'
  end

  test 'the rest of the day is behind a frame only a script fetches' do
    frames = upload_day(frames: 30)
    get "/snapshots/#{frames.first.public_id}"

    assert_match(/<turbo-frame[^>]+id="snapshot-archive"/, response.body)
    assert_match(/<turbo-frame[^>]+loading="lazy"/, response.body,
                 'an eager frame would be fetched by anything that parses HTML')
    assert_match %r{<turbo-frame[^>]+src="[^"]*/snapshots/#{frames.first.public_id}/archive"},
                 response.body
  end

  test 'every frame stays reachable without running the script' do
    frames = upload_day(frames: 30)
    get "/snapshots/#{frames.first.public_id}"

    assert_match %r{href="[^"]*/snapshots/#{frames.first.public_id}/archive"}, response.body,
                 'the strip carries a plain link to the rest, for a reader with no JavaScript'

    get "/snapshots/#{frames.first.public_id}/archive"
    assert_response :success
    assert_equal 30, response.body.scan('class="card-img"').size
  end

  # The frame id is the contract between the two responses. If they drift apart
  # Turbo discards the archive and the strip silently stays at twelve.
  test 'the archive answers into the frame the page opened' do
    frames = upload_day(frames: 30)
    get "/snapshots/#{frames.first.public_id}/archive"

    assert_match(/<turbo-frame[^>]+id="snapshot-archive"/, response.body)
  end

  test 'the slideshow page carries one frame, not the whole day' do
    frames = upload_day(frames: 30)
    get "/snapshots/#{frames.first.public_id}/oneday"
    assert_response :success

    assert_equal 0, response.body.scan('class="carousel-item').size,
                 'the carousel arrives in the frame; writing it here served nobody without JavaScript'
    assert_match %r{<turbo-frame[^>]+src="[^"]*/snapshots/#{frames.first.public_id}/slideshow"},
                 response.body
    assert_operator addresses_in(response.body).size, :<=, 2,
                    'the breadcrumb and the frame source, and no slide list'
  end

  # The same invariant as the show page, asserted the same way: one fixture
  # size proves nothing about growth, and /oneday was the heavier of the two
  # pages before this change -- 130 KB against 95 KB.
  test 'a bigger day does not make the slideshow page hand out more addresses' do
    small = upload_day(frames: 20, mac: '00:11:22:33:44:03')
    get "/snapshots/#{small.first.public_id}/oneday"
    few = addresses_in(response.body).size

    big = upload_day(frames: 96, mac: '00:11:22:33:44:04')
    get "/snapshots/#{big.first.public_id}/oneday"
    many = addresses_in(response.body).size

    assert_equal few, many, <<~MESSAGE.chomp
      A 96-frame day gave away #{many} addresses on /oneday and a 20-frame day
      gave away #{few}. The slideshow is where 96 full-HD URLs and 96 sibling
      ids used to be written out for a client that could not run the carousel.
    MESSAGE
    assert_operator many, :<=, 2, 'the breadcrumb and the frame source'
  end

  # Turbo scopes a link to its enclosing frame. Without target="_top" an icon
  # in the archive would find a frame of the same name in the snapshot page it
  # fetched and swap the grid instead of navigating, and a slide's "link to
  # this" would find no oneday-slideshow frame at all and be refused. Both
  # ends of each pair carry it, because the attribute has to survive the swap.
  test 'links inside the frames navigate the page, not the frame' do
    frames = upload_day(frames: 30)
    id = frames.first.public_id

    {
      "/snapshots/#{id}" => 'snapshot-archive',
      "/snapshots/#{id}/archive" => 'snapshot-archive',
      "/snapshots/#{id}/oneday" => 'oneday-slideshow',
      "/snapshots/#{id}/slideshow" => 'oneday-slideshow'
    }.each do |path, frame|
      get path
      tag = response.body[/<turbo-frame[^>]*id="#{frame}"[^>]*>/]

      assert tag, "#{path} has no #{frame} frame"
      assert_includes tag, 'target="_top"',
                      "#{path}: a link inside #{frame} would navigate the frame instead of the page"
    end
  end

  # The microcache in front of Rails keys on the URI and knows nothing about
  # the Turbo-Frame header, so one address must have one body. When these
  # actions dropped the layout for a frame request, a lazy fetch primed the
  # entry with a bare fragment and the reader who followed the link printed
  # under the strip got an unstyled orphan for the next 300 seconds.
  test 'the frame endpoints answer one body, layout and all' do
    frames = upload_day(frames: 30)
    id = frames.first.public_id

    ["/snapshots/#{id}/archive", "/snapshots/#{id}/slideshow"].each do |path|
      get path
      assert_response :success
      plain = response.body

      get path, headers: { 'Turbo-Frame' => 'snapshot-archive' }
      assert_response :success
      framed = response.body

      # The plain body has to be a real page first. Equality alone would be
      # satisfied by two identically broken responses, and `assert_match
      # /<html/` -- what the first version of this test checked -- is satisfied
      # by the bug itself, because turbo-rails answers from
      # layouts/turbo_rails/frame, which IS an <html> document with an empty
      # <head>.
      %w[stylesheet application og:title canonical].each do |marker|
        assert_includes plain, marker, "#{path} lost #{marker} on a plain GET"
      end

      # Then the invariant itself, stated as the invariant rather than as a
      # sample of it: ONE address, ONE body. Any frame-dependent difference
      # that happened to keep the markers above would pass a marker check and
      # still be cached for every client for 300 seconds. Deterministic here
      # because no page renders a CSRF token since the admin went, so nothing
      # per-request is rendered into these pages.
      assert_equal plain, framed, <<~MESSAGE.chomp
        #{path} answered a different body to a frame request. The proxy cache
        key is $scheme$host|$locale_key|$uri and knows nothing about
        Turbo-Frame, so whichever of the two arrives first is what every other
        client gets for the next 300 seconds. Sizes: #{plain.bytesize} plain,
        #{framed.bytesize} framed.
      MESSAGE
    end
  end

  test 'the slideshow itself still shows the whole day' do
    frames = upload_day(frames: 30)
    get "/snapshots/#{frames.first.public_id}/slideshow"
    assert_response :success

    # `class="carousel-item`, not `carousel-item`: the speed control's script
    # names the same class in a querySelectorAll and would count as a slide.
    assert_equal 30, response.body.scan('class="carousel-item').size
    assert_match(/<turbo-frame[^>]+id="oneday-slideshow"/, response.body)
  end

  # The strip is a screenful; a camera with fewer frames than that has nothing
  # behind it and must not ask for a frame that would render an empty grid.
  test 'a camera with a short day loads no frame at all' do
    frames = upload_day(frames: 3)
    get "/snapshots/#{frames.first.public_id}"

    assert_equal 3, response.body.scan('class="card-img"').size
    assert_no_match(/<turbo-frame[^>]+id="snapshot-archive"[^>]+src=/, response.body)
  end
end
