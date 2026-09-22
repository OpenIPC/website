# frozen_string_literal: true

require 'test_helper'

# The JSON the prerendered home page fills its mosaic from (#160).
module Api
  module V1
    class WallControllerTest < ActionDispatch::IntegrationTest
      # There are no snapshot fixtures, and latest_per_camera only looks at the
      # last 24 hours, so every test that needs a tile makes one. The blob
      # validator wants a real image type and at least ten kilobytes.
      def upload(mac:, soc: 'gk7205v300', sensor: 'imx335', generated: true)
        snapshot = Snapshot.new(mac_address: mac, ip_address: '198.51.100.7', soc: soc, sensor: sensor)
        snapshot.file.attach(io: StringIO.new("\xFF\xD8\xFF#{'x' * 12_000}"),
                             filename: 'wall.jpg', content_type: 'image/jpeg')
        snapshot.save!
        snapshot.update_columns(variants_generated_at: generated ? Time.current : nil)
        snapshot
      end

      test 'it answers JSON and says how long it may be kept' do
        get '/api/v1/wall/latest'

        assert_response :success
        assert_equal 'application/json', response.media_type
        # Every visitor to the home page asks for this, and the query behind it
        # is the greatest-n-per-group join Snapshot documents at ~280ms.
        assert_match(/max-age=60/, response.headers['Cache-Control'])
        assert_match(/public/, response.headers['Cache-Control'])
      end

      test 'an empty wall is an empty list, not an error' do
        Snapshot.delete_all

        get '/api/v1/wall/latest'

        assert_response :success
        assert_equal [], response.parsed_body['snapshots']
      end

      test 'it never returns more tiles than the mosaic holds' do
        Snapshot.delete_all
        7.times { |i| upload(mac: format('02:00:00:00:00:%02d', i)) }

        get '/api/v1/wall/latest'

        assert_operator response.parsed_body['snapshots'].size, :<=, WallController::TILES
      end

      test 'a tile names a page, an image under /wall/, and a caption' do
        Snapshot.delete_all
        upload(mac: '02:00:00:00:00:10')

        get '/api/v1/wall/latest'
        tile = response.parsed_body['snapshots'].find { |t| t['caption'] == 'GK7205V300 · IMX335' }

        assert tile, "no tile for the snapshot: #{response.parsed_body['snapshots'].inspect}"
        assert_match %r{\A/wall/}, tile['src'],
                     'the image is not the file nginx serves, so every tile would wake Rails'
        assert_match %r{/snapshots/}, tile['href']
      end

      test 'a camera that named neither its soc nor its sensor still gets a tile' do
        # Both are nullable and the upload endpoint permits either to be
        # absent. One such upload used to take the whole homepage down with it.
        Snapshot.delete_all
        upload(mac: '02:00:00:00:00:11', soc: nil, sensor: nil)

        get '/api/v1/wall/latest'

        assert_response :success
        assert_includes response.parsed_body['snapshots'].map { |t| t['caption'] }, ''
      end

      test 'a snapshot whose variants are not written yet is left out' do
        # Its files do not exist, so a tile would be a broken image. The page
        # pads with the same placeholder it uses for a camera that never
        # uploaded.
        Snapshot.delete_all
        upload(mac: '02:00:00:00:00:12', generated: false)

        get '/api/v1/wall/latest'

        assert_empty response.parsed_body['snapshots']
      end
    end
  end
end
