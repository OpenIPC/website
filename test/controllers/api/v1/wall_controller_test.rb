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
    get '/api/v1/wall/mosaic.json', params: { limit: 5 }

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

  test 'the number of tiles is bounded whatever is asked for' do
    get '/api/v1/wall/mosaic.json', params: { limit: 10_000 }

    assert_response :success
    assert_operator response.parsed_body['tiles'].size, :<=, Api::V1::WallController::MAX_TILES
  end

  test 'a limit that is not a number is one tile, not none and not all of them' do
    get '/api/v1/wall/mosaic.json', params: { limit: 'all' }

    assert_response :success
    assert_equal 1, response.parsed_body['tiles'].size
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
end
