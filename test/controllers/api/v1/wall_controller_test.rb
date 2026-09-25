# frozen_string_literal: true

require 'test_helper'

# The address a prerendered page asks for its mosaic (#165).
#
# What it must never become is a way to pull frames a wall page would not have
# shown. Every test here is about that boundary as much as about the payload.
class Api::V1::WallControllerTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  # Three cameras, because `latest_per_camera` is one row per MAC and a single
  # snapshot would not tell a working query from a broken one.
  setup do
    @snapshots = 3.times.map do |i|
      snapshot = Snapshot.new(mac_address: format('00:11:22:33:44:%02x', 0xB0 + i),
                              ip_address: '203.0.113.8', soc: 'gk7205v300', sensor: 'imx307')
      snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                           content_type: 'image/jpeg')
      snapshot.save!(validate: false)
      snapshot
    end
  end

  test 'it answers with tiles and a grant that names them' do
    get '/api/v1/wall/mosaic.json'

    assert_response :success
    body = response.parsed_body

    assert_equal 'thumb', body['variant']
    assert_equal 3, body['tiles'].size

    granted = WallGrant.verify(body['grant'])
    assert granted, 'the grant does not verify'
    assert_equal body['tiles'].map { |t| WallGrant.pair(t['id'], 'thumb') }.sort, granted.sort,
                 'the grant names something other than the tiles it was issued with'
  end

  test 'a tile carries what the caption is drawn from and nothing else' do
    get '/api/v1/wall/mosaic.json'

    assert_response :success
    response.parsed_body['tiles'].each do |tile|
      assert_equal %w[id sensor soc].sort, tile.keys.sort,
                   'a tile carries a field the mosaic does not draw'
      assert_match(/\A[a-z0-9]+\z/, tile['id'])
    end
  end

  test 'it never authorises a variant a wall page did not draw at' do
    # `fullhd` is the snapshot page's, granted for the one frame it has just
    # shown. Reachable from here it would let a caller pull a full-resolution
    # frame of a camera the site only ever put on a 480px tile.
    get '/api/v1/wall/mosaic.json', params: { variant: 'fullhd' }

    assert_response :success
    assert_equal 'thumb', response.parsed_body['variant']
    WallGrant.verify(response.parsed_body['grant']).each do |pair|
      assert_equal 'thumb', pair.split(WallGrant::SEPARATOR).last
    end
  end

  test 'the number of tiles is the server\'s, and no query changes it' do
    # It was `?limit=`, and both vhosts cache this on $uri, which drops the
    # query -- so whichever count filled the cache first was the one every home
    # page got for the next minute, and the grant with it. A parameter that
    # changes the body has to be in the cache key or not exist.
    get '/api/v1/wall/mosaic.json', params: { limit: 24 }
    many = response.parsed_body

    get '/api/v1/wall/mosaic.json', params: { limit: 1 }
    few = response.parsed_body

    # Three cameras exist, so three come back however many are asked for.
    assert_operator many['tiles'].size, :<=, Api::V1::WallController::TILES
    assert_equal many, few, 'the query string changes a response cached without it'
  end

  test 'it is readable from a mirror' do
    # openipc.kz and openipc.cloud serve this bundle from their own hosts. A
    # versioned public API only the canonical origin may read is one they
    # cannot use, and there is nothing here that is not already in
    # /open-wall's HTML.
    get '/api/v1/wall/mosaic.json'

    assert_equal '*', response.headers['Access-Control-Allow-Origin']
  end

  test 'no cameras is an empty mosaic and no grant, not an error' do
    Snapshot.delete_all

    get '/api/v1/wall/mosaic.json'

    assert_response :success
    assert_empty response.parsed_body['tiles']
    assert_nil response.parsed_body['grant'],
               'a grant naming nothing would be a permission to ask for nothing'
  end

  test 'it is cacheable, because one body serves every reader' do
    # The home page is the busiest page on the site and this is fetched on every
    # view of it. The grant is bucketed to the cache window for exactly this
    # reason -- see WallGrant::CACHE_WINDOW.
    get '/api/v1/wall/mosaic.json'

    assert_response :success
    assert_includes response.headers['Cache-Control'], 'public'
    assert_includes response.headers['Cache-Control'], 'max-age=60'
  end

  test 'two calls in the same bucket sign to the same grant' do
    get '/api/v1/wall/mosaic.json'
    first = response.parsed_body['grant']
    get '/api/v1/wall/mosaic.json'

    assert_equal first, response.parsed_body['grant'],
                 'the body changes between renders, which a microcache would freeze at random'
  end

  test 'it carries no address that returns an image' do
    # #267 removed every one of them. An endpoint handing out image URLs would
    # put back exactly what that change took away.
    get '/api/v1/wall/mosaic.json'

    assert_no_match(%r{/wall/|\.jpe?g|\.webp|rails/active_storage}, response.body)
  end

  # --- the wall's own pages, as data (#165) ----------------------------------
  #
  # These replace Rails-rendered pages, so the test that matters most is not
  # what they return but what they authorise: each one must grant exactly what
  # the page at the same address grants today, no more. A harvester's economics
  # must not improve because a page moved into the bundle.

  # One camera with a day of frames, for the strip, the archive and the
  # slideshow. Times are set explicitly because ordering is the thing under
  # test and three rows saved in the same second have none.
  def camera_day(count, mac: '00:11:22:33:44:FF')
    Array.new(count) do |i|
      snapshot = Snapshot.new(mac_address: mac, ip_address: '203.0.113.9',
                              soc: 'hi3516ev300', sensor: 'imx335', firmware: 'lite',
                              streamer: 'majestic', uptime: '3 days', caption: 'a yard')
      snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                           content_type: 'image/jpeg')
      snapshot.save!(validate: false)
      snapshot.update_column(:created_at, (count - i).minutes.ago)
      snapshot.reload
    end
  end

  test 'the page endpoint answers the wall a page at a time' do
    get '/api/v1/wall/page/1.json'

    assert_response :success
    body = response.parsed_body

    assert_equal 1, body['page']
    assert_equal 3, body['tiles'].size
    assert_operator body['tiles'].size, :<=, Api::V1::WallController::PER_PAGE
    assert_equal body['tiles'].map { |t| WallGrant.pair(t['id'], 'thumb') }.sort,
                 WallGrant.verify(body['grant']).sort
  end

  test 'a page past the end is empty rather than wrong' do
    get '/api/v1/wall/page/99.json'

    assert_response :success
    assert_empty response.parsed_body['tiles']
  end

  test 'a wall card carries what the card prints' do
    get '/api/v1/wall/page/1.json'

    response.parsed_body['tiles'].each do |tile|
      assert_equal %w[at bytes dimensions firmware id sensor soc soc_temperature streamer uptime].sort,
                   tile.keys.sort
    end
  end

  test 'the page size is the wall\'s, not this controller\'s' do
    # The page moves into the bundle; the number of cards on it does not
    # change, and neither does the number of ids one fetch is worth.
    assert_equal SnapshotsController::PER_PAGE, Api::V1::WallController::PER_PAGE
    assert_equal SnapshotsController::STRIP_EAGER, Api::V1::WallController::STRIP_EAGER
  end

  test 'the snapshot endpoint grants the frame at fullhd and its strip at icon2' do
    frames = camera_day(4)
    subject = frames.last

    get "/api/v1/wall/snapshot/#{subject.public_id}.json"

    assert_response :success
    body = response.parsed_body

    assert_equal subject.public_id, body['snapshot']['id']
    assert_equal 4, body['strip'].size
    refute body['strip_more'], 'four frames is under the eager limit'

    granted = WallGrant.verify(body['grant'])
    assert_includes granted, WallGrant.pair(subject.public_id, 'fullhd')
    body['strip'].each { |icon| assert_includes granted, WallGrant.pair(icon['id'], 'icon2') }
    assert_equal 1, granted.count { |pair| pair.end_with?('fullhd') },
                 'only the frame being shown may be granted at full resolution'
  end

  test 'the strip stops at the eager limit and says there is more' do
    # The supply line #235 closed: a page that names every frame of the day
    # hands a harvester a fresh batch of addresses for free.
    frames = camera_day(Api::V1::WallController::STRIP_EAGER + 3)

    get "/api/v1/wall/snapshot/#{frames.last.public_id}.json"

    body = response.parsed_body
    assert_equal Api::V1::WallController::STRIP_EAGER, body['strip'].size
    assert body['strip_more']
    assert_equal Api::V1::WallController::STRIP_EAGER + 1, WallGrant.verify(body['grant']).size
  end

  test 'a strip icon carries an address and a time, and no hardware' do
    frames = camera_day(3)

    get "/api/v1/wall/snapshot/#{frames.last.public_id}.json"

    response.parsed_body['strip'].each do |icon|
      assert_equal %w[at id].sort, icon.keys.sort,
                   'a day of frames repeating one camera\'s hardware is a description of it'
    end
  end

  test 'the archive is the whole day at icon2 and nothing larger' do
    frames = camera_day(5)

    get "/api/v1/wall/snapshot/#{frames.last.public_id}/archive.json"

    assert_response :success
    body = response.parsed_body

    assert_equal 'icon2', body['variant']
    assert_equal 5, body['frames'].size
    times = body['frames'].map { |f| f['at'] }
    assert_equal times.sort.reverse, times, 'the archive is newest first, as the page is'
    WallGrant.verify(body['grant']).each { |pair| assert pair.end_with?('icon2') }
  end

  test 'the slideshow is the whole day at fullhd, oldest first' do
    # Truncating this was considered and rejected on 2026-09-24: FRAME_BUDGET
    # is the binding constraint, so a cap costs a feature and buys nothing.
    frames = camera_day(5)

    get "/api/v1/wall/snapshot/#{frames.last.public_id}/slideshow.json"

    assert_response :success
    body = response.parsed_body

    assert_equal 'fullhd', body['variant']
    times = body['frames'].map { |f| f['at'] }
    assert_equal times.sort, times
    WallGrant.verify(body['grant']).each { |pair| assert pair.end_with?('fullhd') }
  end

  test 'a camera token answers with that camera\'s newest frame' do
    frames = camera_day(3)

    get "/api/v1/wall/camera/#{frames.last.camera_token}.json"

    assert_response :success
    body = response.parsed_body

    assert_equal frames.last.public_id, body['snapshot']['id'],
                 'the token resolves to the camera\'s newest frame, as the page does'
    assert_equal 3, body['strip'].size
    refute_includes response.body, frames.last.mac_address,
                    'the MAC is the join key and must never leave the server'
  end

  test 'a camera nobody has is missing rather than redirected' do
    get "/api/v1/wall/camera/#{'0' * 16}.json"

    assert_response :not_found
  end

  test 'a numeric id is gone and an unknown one is missing' do
    # The same answers SnapshotsController gives, so moving the page does not
    # change what a stale link does.
    get '/api/v1/wall/snapshot/12345.json'
    assert_response :gone

    get "/api/v1/wall/snapshot/#{'a' * 20}.json"
    assert_response :not_found
  end

  test 'every wall address is cacheable and readable from a mirror' do
    frames = camera_day(2)
    id = frames.last.public_id

    ['/api/v1/wall/page/1.json', "/api/v1/wall/snapshot/#{id}.json",
     "/api/v1/wall/snapshot/#{id}/archive.json", "/api/v1/wall/snapshot/#{id}/slideshow.json"].each do |path|
      get path

      assert_response :success, path
      assert_includes response.headers['Cache-Control'], 'max-age=60', path
      assert_equal '*', response.headers['Access-Control-Allow-Origin'], path
      assert_no_match(%r{/wall/|\.jpe?g|\.webp|rails/active_storage}, response.body, path)
    end
  end
end
