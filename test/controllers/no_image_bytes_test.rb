# frozen_string_literal: true

require 'test_helper'

# No public address returns image bytes.
#
# This file exists because the Open Wall has been hardened four times and every
# attempt was defeated by a door somebody forgot, not by the door they were
# working on:
#
#   #235  opaque ids            -> the pages handed the new ids straight back
#   #261  strip behind a frame  -> /archive was left as a plain link, and the
#                                  crawler followed it; ids harvested went UP,
#                                  2,255 -> 3,188
#   #262  Azure address block   -> the fleet moved off Azure inside a day
#
# And underneath all three, unnoticed the whole time, /snapshots/<id>/download
# was handing over the ORIGINAL upload -- full resolution, EXIF unstripped. In
# the 24 hours to 2026-09-23 it was asked for 18,007 times and served 5,825
# times, 1,182 MB, covering 2,341 of the 2,820 snapshots that existed. 83% of
# every frame on the wall. Fourteen of the 10,630 requesting addresses had ever
# executed the page beacon.
#
# Nothing in the suite failed when that route was deleted, because nothing
# tested it. That is the whole point of this file: the guarantee is a property
# of the WHOLE route table, so it is asserted over the whole route table rather
# than route by route. A new action that serves bytes fails here on the day it
# is written, not a month later in an access log.
class NoImageBytesTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b
  IMAGE_MAGIC = ["\xFF\xD8\xFF".b, "\x89PNG".b].freeze

  def snapshot
    @snapshot ||= begin
      s = Snapshot.new(mac_address: '00:11:22:33:44:99', ip_address: '203.0.113.9',
                       soc: 'gk7205v300', sensor: 'imx307')
      s.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                    content_type: 'image/jpeg')
      s.save!(validate: false)
      s
    end
  end

  # The addresses that used to return bytes, in every spelling the site has.
  # Written out as literal URLs rather than built from helpers, because the
  # helpers for three of them no longer exist -- and a test that cannot be
  # written with a route helper is exactly the test worth having.
  LOCALES = ['', '/ru', '/zh'].freeze

  def retired_byte_paths
    id = snapshot.public_id
    token = snapshot.camera_token
    localised = LOCALES.flat_map do |l|
      ["#{l}/snapshots/#{id}/download", "#{l}/open-wall/camera/#{token}.jpg"]
    end

    localised + ["/snapshots/#{id}/download.jpg", "/snapshots/camera.jpg?id=#{token}",
                 "/wall/#{id}/thumb.jpg", "/wall/#{id}/fullhd.jpg",
                 "/rails/active_storage/blobs/redirect/#{id}/x.jpg",
                 "/rails/active_storage/disk/#{id}/x.jpg"]
  end

  test 'no retired byte path serves an image' do
    retired_byte_paths.each do |path|
      get path
      assert_not_image path
    end
  end

  # The camera page's format guard, locked separately.
  #
  # Restoring `get :camera, on: :collection` in routes.rb does NOT reopen the
  # byte path -- verified by putting it back and watching this file stay green
  # -- because the action refuses anything that is not HTML. That makes the
  # guard the load-bearing part rather than the routing, so it gets its own
  # assertion: a regression that answered .jpg with a render, or died on a
  # missing template and returned 500, would both pass the no-bytes test and
  # both be wrong.
  test 'the camera page answers only HTML' do
    token = snapshot.camera_token

    get "/open-wall/camera/#{token}"
    assert_response :success
    assert_equal 'text/html', response.media_type

    get "/open-wall/camera/#{token}.jpg"
    assert_response :not_found
  end

  # The guarantee, stated over the whole route table rather than a list.
  #
  # Every GET-able route with no dynamic segment beyond :id and :locale is
  # requested, and none may answer with an image. A route added later that
  # serves bytes fails here without anybody remembering to add it to a list.
  test 'no public GET route anywhere answers with image bytes' do
    checked = 0

    public_get_routes.each do |path|
      get path
      assert_not_image path
      checked += 1
    rescue ActionController::RoutingError, ActionView::Template::Error,
           AbstractController::ActionNotFound, ActiveRecord::RecordNotFound
      # A route with no action behind it (resources :snapshots generates
      # /snapshots/new and nothing implements it), or one whose :id belongs to
      # another model so a snapshot id finds nothing (/cameras/socs/:id). A
      # request that raises before rendering returned no bytes.
      #
      # This does narrow the walk: a byte-serving route keyed on some other
      # model would be skipped here. The retired-paths test above covers the
      # gallery explicitly, and the wall is the only place this app serves
      # user images from.
      next
    end

    assert_operator checked, :>, 20, 'the route walk found almost nothing -- it is not doing its job'
  end

  # A snapshot page must not so much as name a byte path, because the crawler
  # reads the markup and follows what it finds. #261 is the precedent: the one
  # link left for non-JS readers was the whole leak.
  test 'no rendered page mentions an address that returns bytes' do
    id = snapshot.public_id

    ['/open-wall', '/', "/snapshots/#{id}", "/snapshots/#{id}/archive",
     "/snapshots/#{id}/oneday", "/snapshots/#{id}/slideshow"].each do |path|
      get path
      next unless response.successful?

      # /rails/active_storage is in this list as of the transport change. It
      # was the quietest of the doors: Snapshot#wall_image fell back to an
      # ActiveStorage variant URL whenever variants_generated_at? was nil --
      # every frame between upload and ProcessImagesJob -- so even after #146
      # moved the files, freshly uploaded frames were fetchable over HTTP. The
      # fallback is gone and the engine's routes answer 410.
      %w[/download camera.jpg /rails/active_storage /wall/].each do |needle|
        assert_not_includes response.body, needle,
                            "#{path} still names #{needle}, which is a door a crawler will walk through"
      end
    end
  end

  private

  def public_get_routes
    specs = Rails.application.routes.routes.filter_map do |route|
      route.path.spec.to_s.delete_suffix('(.:format)') if route.verb.to_s.include?('GET')
    end

    specs.select { |spec| walkable?(spec) }.map { |spec| concrete(spec) }.uniq
  end

  # Skippable: the ActiveStorage engine (its own step), globs, and anything
  # needing a segment we cannot fill.
  def walkable?(spec)
    return false if spec.start_with?('/rails') || spec.include?('*')

    spec.scan(/:(\w+)/).flatten.all? { |seg| %w[id locale].include?(seg) }
  end

  def concrete(spec)
    spec.sub('(/:locale)', '').sub('(:locale)', '').sub(':id', snapshot.public_id).squeeze('/')
  end

  # Judge the BODY, not the header. A bodyless `head :not_found` inherits the
  # content type of whatever was asked for, so asserting on media_type alone
  # failed a route that was correctly refusing. What matters is whether image
  # bytes came back.
  def assert_not_image(path)
    body = response.body.to_s.b
    served = response.successful? && response.media_type.to_s.start_with?('image/')

    assert_not(body.start_with?(*IMAGE_MAGIC) || served,
               "#{path} answered #{response.status} with #{body.bytesize} bytes " \
               'of image -- this address returns image bytes')
  end
end
