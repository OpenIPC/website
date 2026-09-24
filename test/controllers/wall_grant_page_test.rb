# frozen_string_literal: true

require 'test_helper'

# Every page that draws frames must also authorise them.
#
# The channel now refuses a subscription without a grant, which is the point --
# 94% of the clients measured on it had never loaded a page. The cost of that
# is a new way to break the wall for real readers: a surface that renders
# canvases but emits no grant looks perfectly fine in review, renders perfectly
# fine in a browser, and paints nothing.
#
# There are seven such surfaces and they do not share an action. The grant is
# emitted by the layout for most of them and from inside the frame for the two
# lazy turbo-frames, so "it works on the gallery" is not evidence that it works
# on the slideshow. This file checks the property on each page that has one.
class WallGrantPageTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  setup do
    @snapshot = Snapshot.new(mac_address: '00:11:22:33:44:98', ip_address: '203.0.113.8',
                             soc: 'gk7205v300', sensor: 'imx307')
    @snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                          content_type: 'image/jpeg')
    @snapshot.save!(validate: false)

    WallImage::VARIANTS.each { |v| WallImage.store_bytes(@snapshot.public_id, v, MINIMAL_JPEG) }
  end

  teardown do
    WallImage.purge(@snapshot.public_id)
  end

  # The ids the page actually drew, read back out of the markup rather than
  # assumed, so this cannot drift from what the helper emits.
  def rendered_ids
    response.body.scan(/data-wall-frame="([0-9a-f]+)"/).flatten.uniq
  end

  def grant_on_page
    response.body[/data-wall-grant="([^"]+)"/, 1]
  end

  def assert_grant_covers_page(where)
    ids = rendered_ids
    assert_not_empty ids, "#{where} drew no frames, so this test is not checking anything"

    token = grant_on_page
    assert token, "#{where} drew #{ids.size} frames and issued no grant. " \
                  'The channel will refuse every one of them and the page will paint nothing.'

    granted = WallGrant.verify(CGI.unescapeHTML(token))
    assert granted, "#{where} issued a grant that does not verify"

    missing = ids - granted[:ids].to_a
    assert_empty missing,
                 "#{where} drew #{missing.size} frames its own grant does not cover: " \
                 "#{missing.first(3).join(', ')}"
  end

  test 'the gallery authorises the frames it draws' do
    get open_wall_path
    assert_response :success
    assert_grant_covers_page 'the gallery'
  end

  test 'a snapshot page authorises the frames it draws' do
    get snapshot_path(id: @snapshot.public_id)
    assert_response :success
    assert_grant_covers_page 'the snapshot page'
  end

  test 'the homepage mosaic authorises the frames it draws' do
    get root_path
    assert_response :success
    assert_grant_covers_page 'the homepage'
  end

  # The two lazy turbo-frames are the ones the layout cannot help. Turbo keeps
  # only the <turbo-frame> element from the response and discards everything
  # around it, so a grant emitted after </main> never reaches the document --
  # the reader would be left holding the previous page's permission, which
  # names none of the 96 frames that just arrived.
  test 'the archive frame carries its own grant, inside the frame' do
    get archive_snapshot_path(id: @snapshot.public_id)
    assert_response :success
    assert_grant_covers_page 'the archive frame'

    frame = response.body[/<turbo-frame[^>]*id="snapshot-archive".*?<\/turbo-frame>/m]
    assert frame, 'the archive did not render its turbo-frame'
    assert_includes frame, 'data-wall-grant',
                    'The grant is outside the turbo-frame, so Turbo will throw it away ' \
                    'and the archive will paint nothing when loaded lazily.'
  end

  test 'the slideshow frame carries its own grant, inside the frame' do
    get slideshow_snapshot_path(id: @snapshot.public_id)
    assert_response :success
    assert_grant_covers_page 'the slideshow frame'

    frame = response.body[/<turbo-frame[^>]*id="oneday-slideshow".*?<\/turbo-frame>/m]
    assert frame, 'the slideshow did not render its turbo-frame'
    assert_includes frame, 'data-wall-grant',
                    'The grant is outside the turbo-frame, so Turbo will throw it away ' \
                    'and the slideshow will paint nothing when loaded lazily.'
  end

  # A grant is a standing permission to read somebody's camera, so it must not
  # be handed to pages that have no frames on them -- which is most of the site.
  test 'a page with no frames issues no grant' do
    get donate_path
    assert_response :success

    assert_nil grant_on_page
  end
end
