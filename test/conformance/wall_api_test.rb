# frozen_string_literal: true

require_relative 'conformance_helper'

# The Open Wall's JSON addresses (#296), which the static bundle's pages read
# and nginx microcaches. Nothing in the suite covered them before the Go
# service had to answer them: their shape, their headers, and what each one
# authorises, against Rails and against Go alike.
#
# The data is made the way cameras make it -- by uploading -- so the same test
# works on Rails' MariaDB and on Go's PostgreSQL without either needing rows it
# did not write. A camera needs several frames for a strip, and the interval
# allows one per fifteen minutes, so the frames come from the whitelisted
# address: reachable only when the suite talks to the application directly.
class WallApiConformanceTest < Conformance::Case
  surface :wall

  def setup
    super
    skip 'needs CONFORMANCE_DATABASE_URL: this test uploads frames it must delete' unless Conformance.database?
    skip 'needs CONFORMANCE_WHITELISTED_IP to upload several frames per camera' if whitelisted.empty?
    skip 'only reachable talking to the application directly' unless Conformance.direct?
  end

  def whitelisted
    ENV['CONFORMANCE_WHITELISTED_IP'].to_s
  end

  # Frames for one camera, oldest first; returns their ids.
  def frames(mac, count, **fields)
    Array.new(count) do
      response = upload(mac: mac, headers: { 'X-Forwarded-For' => whitelisted }, **fields)
      assert_equal '201', response.code, response['X-Error']
      response['Location'].split('/').last
    end
  end

  def json(path)
    response = get(path)
    [response, response.code == '200' ? JSON.parse(response.body) : nil]
  end

  def assert_wall_headers(response, path)
    assert_equal '200', response.code, path
    assert_equal 'max-age=60, public', response['Cache-Control'], "#{path}: nginx caches what this says"
    assert_equal '*', response['Access-Control-Allow-Origin'], "#{path}: the mirrors read it"
    assert_match %r{\Aapplication/json}, response['Content-Type'], path
  end

  test 'the mosaic: five tiles at most, the newest frame of each camera, and a thumb grant' do
    ids = frames(fresh_mac, 2, soc: 'hi3516ev300', sensor: 'imx335')

    response, body = json('/api/v1/wall/mosaic.json')

    assert_wall_headers response, 'mosaic'
    assert_equal %w[variant grant tiles], body.keys, 'the key order is what the bytes are'
    assert_equal 'thumb', body['variant']
    assert body['tiles'].size.between?(1, 5)
    assert_equal %w[id soc sensor], body['tiles'].first.keys
    ours = body['tiles'].find { |t| t['id'] == ids.last }
    assert ours, 'the newest frame of a camera that just uploaded is a tile'
    assert_equal({ 'id' => ids.last, 'soc' => 'hi3516ev300', 'sensor' => 'imx335' }, ours)
    refute(body['tiles'].any? { |t| t['id'] == ids.first }, 'one tile per camera, not per frame')
    assert_kind_of String, body['grant']
  end

  test 'a page of the gallery: cards in their order, and nothing past the end' do
    ids = frames(fresh_mac, 1, soc: 'gk7205v300', sensor: 'sc2315e', firmware: '2.6.09.15-lite',
                               streamer: 'majestic', uptime: '8 days', soc_temperature: '55.73')

    response, body = json('/api/v1/wall/page/1.json')

    assert_wall_headers response, 'page 1'
    assert_equal %w[variant page pages tiles grant], body.keys
    assert_equal ['thumb', 1], [body['variant'], body['page']]
    card = body['tiles'].find { |t| t['id'] == ids.first }
    assert card, 'the frame is on page one'
    assert_equal %w[id soc sensor firmware streamer uptime soc_temperature dimensions bytes at], card.keys
    assert_equal ['gk7205v300', 'sc2315e', '2.6.09.15-lite', 'majestic', '8 days', '55.73', 12_288],
                 card.values_at('soc', 'sensor', 'firmware', 'streamer', 'uptime', 'soc_temperature', 'bytes')
    assert_match(/\A(\d+x\d+|x)\z/, card['dimensions'], 'width x height, or "x" before the image is measured')
    assert_in_delta Time.now.to_i, card['at'], 120

    _, past = json("/api/v1/wall/page/#{body['pages'] + 1}.json")
    assert_equal [], past['tiles']
    assert_nil past['grant'], 'nothing to draw is nothing to grant'
  end

  test 'a snapshot: the frame, the first screenful of its camera\'s day, and whether there is more' do
    mac = fresh_mac
    ids = frames(mac, 3, soc: 'ssc30kq', sensor: 'sc4336p', caption: '')
    subject = ids.last

    response, body = json("/api/v1/wall/snapshot/#{subject}.json")

    assert_wall_headers response, 'snapshot'
    assert_equal %w[variant strip_variant snapshot strip strip_more grant], body.keys
    assert_equal %w[fullhd icon2], body.values_at('variant', 'strip_variant')
    assert_equal subject, body['snapshot']['id']
    assert_equal %w[caption camera], body['snapshot'].keys.last(2)
    assert_nil body['snapshot']['caption'], 'a blank caption is null'
    assert_match(/\A[0-9a-f]{16}\z/, body['snapshot']['camera'])
    assert_equal ids.reverse, body['strip'].map { |f| f['id'] }, 'the day, newest first'
    assert_equal %w[id at], body['strip'].first.keys, 'an icon is an address and a time, nothing about the camera'
    assert_equal false, body['strip_more']

    camera = body['snapshot']['camera']
    response, by_token = json("/api/v1/wall/camera/#{camera}.json")
    assert_equal '200', response.code
    assert_equal subject, by_token['snapshot']['id'], 'the camera permalink resolves to its newest frame'
  end

  test 'a day as an archive and as a slideshow' do
    ids = frames(fresh_mac, 3)

    response, archive = json("/api/v1/wall/snapshot/#{ids[1]}/archive.json")
    assert_wall_headers response, 'archive'
    assert_equal %w[variant frames grant], archive.keys
    assert_equal 'icon2', archive['variant']
    assert_equal(ids.reverse, archive['frames'].map { |f| f['id'] })

    _, slides = json("/api/v1/wall/snapshot/#{ids[1]}/slideshow.json")
    assert_equal 'fullhd', slides['variant']
    assert_equal ids, slides['frames'].map { |f| f['id'] }, 'a slideshow runs oldest first'
  end

  test 'a retired numeric id is gone, an unknown one is missing, and neither has a body' do
    { '/api/v1/wall/snapshot/12345.json' => '410', '/api/v1/wall/snapshot/12345/archive.json' => '410',
      '/api/v1/wall/snapshot/0123456789abcdef0123.json' => '404',
      '/api/v1/wall/camera/0123456789abcdef.json' => '404' }.each do |path, code|
      response = get(path)
      assert_equal code, response.code, path
      assert_equal '', response.body.to_s, path
    end
  end

  # nginx caches one body for a minute and serves it to everyone, so a body
  # that changed from one render to the next would be a cache miss every time
  # -- correct, and five times more expensive under a flood. The grant's expiry
  # is bucketed to five minutes for exactly this.
  test 'two fetches inside one grant window are byte-identical' do
    frames(fresh_mac, 1)
    2.times do
      window = Time.now.to_i / 300
      first = get('/api/v1/wall/mosaic.json').body
      second = get('/api/v1/wall/mosaic.json').body
      next unless Time.now.to_i / 300 == window # straddled a boundary; once more

      assert_equal first, second
      return
    end
    flunk 'could not fetch twice inside one five-minute window'
  end
end
