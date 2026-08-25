# frozen_string_literal: true

require 'test_helper'

# The gallery is the page /majestic-endpoints now sends readers to, and it is
# nothing but asset references. Production runs with config.assets.compile =
# false, so a reference the pipeline does not know about does not fall back to
# anything -- it 404s, and the page renders as a wall of broken images. Fetching
# every URL the page emits is the only check that catches that before deploy.
class WebInterfaceTest < ActionDispatch::IntegrationTest
  setup do
    get '/web-interface'
    assert_response :success
    @body = response.body
    @screens = WebuiGallery.screens
  end

  test 'it shows one tile per screenshot in the manifest' do
    assert_operator @screens.size, :>, 0, 'the manifest lists no screenshots'
    assert_select 'figure img.img-zoom', @screens.size
  end

  test 'every thumbnail the page references is served' do
    srcs = @body.scan(/<img[^>]+src="([^"]+)"/).flatten.select { |s| s.include?('webui/') }
    assert_equal @screens.size, srcs.size, 'expected one thumbnail per screenshot'

    srcs.each do |src|
      get src
      assert_response :success, "thumbnail 404s in the asset pipeline: #{src}"
    end
  end

  test 'every zoom target the page references is served' do
    zooms = @body.scan(/data-zoom="([^"]+)"/).flatten
    assert_equal @screens.size, zooms.size, 'expected one full-size image per screenshot'

    zooms.each do |zoom|
      get zoom
      assert_response :success, "zoom image 404s in the asset pipeline: #{zoom}"
    end
  end

  test 'the zoom target is a different file from the tile' do
    # If these ever collapse to the same file the modal is showing an upscaled
    # thumbnail, which is the soft-on-Retina problem the two sizes exist to fix.
    tiles = @body.scan(/<img[^>]+src="([^"]+)"/).flatten.select { |s| s.include?('webui/') }
    zooms = @body.scan(/data-zoom="([^"]+)"/).flatten

    tiles.zip(zooms).each do |tile, zoom|
      assert_not_equal tile, zoom, "tile and zoom point at the same file: #{tile}"
      assert_includes tile, '-thumb', "the tile should be the downscaled copy: #{tile}"
    end
  end

  test 'no screenshot is still referenced as a jpg' do
    # The gallery was 1x JPEGs of a WebUI that has since been redesigned. A
    # leftover reference would 404 rather than show an out-of-date picture.
    assert_no_match(%r{webui/[^"]+\.jpg}, @body)
    assert_empty Dir.glob(Rails.root.join('app/assets/images/webui/*.jpg')),
                 'old 1x JPEG screenshots are still in the repository'
  end

  test 'each tile carries alt text naming the page it shows' do
    assert_select 'figure img.img-zoom' do |imgs|
      imgs.each do |img|
        assert_match(/web interface\z/, img['alt'].to_s,
                     "tile is missing descriptive alt text: #{img['src']}")
      end
    end
  end

  test 'the captions are the page titles the camera prints' do
    # Not translated on purpose: the WebUI is English-only, so the caption is
    # what a reader matches against on the device.
    @screens.each do |screen|
      assert_select 'figcaption', text: screen.caption
    end
  end

  test 'the borrowed preview scene is credited on the page' do
    # Two of the shots do not show what the lab camera is pointed at: the beach
    # in the player belongs to somebody else, given to us for this. A reader has
    # no way to tell a substituted scene from a real one, so the page has to say
    # whose it is and link the permission. Scoped to the article, and to a URL
    # that appears nowhere else, so neither half of this can be satisfied by the
    # layout.
    assert_select 'article p a[href=?]',
                  'https://github.com/OpenIPC/majestic/issues/300#issuecomment-5405996706'
    assert_select 'article', /@usa-/
  end
end
