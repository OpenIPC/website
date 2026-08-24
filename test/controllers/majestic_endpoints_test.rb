# frozen_string_literal: true

require 'test_helper'

# The page used to carry the endpoint list in full. It fell behind the firmware
# -- /ws/video, /nwebrtc, /api/v1/config.schema.json, /night/ircut and
# /night/light all shipped without reaching it -- while the camera's own WebUI
# builds the same list from the running build. So the list lives there now and
# the page says where it went.
#
# The URL stays: it took roughly sixty-five requests a day in the fortnight
# before this change, about half of them human, arriving from Google, from
# OpenIPC/wiki and from other people's forum posts. Those are links we do not
# control and should not break.
class MajesticEndpointsTest < ActionDispatch::IntegrationTest
  test 'the page still answers, so the inbound links keep working' do
    get '/majestic-endpoints'
    assert_response :success
  end

  # Both assertions here are scoped to the article on purpose. Unscoped, the
  # first was satisfied by the About dropdown in the layout, which links
  # /web-interface on every page of the site, and the second by the <title>,
  # which PagesController sets from the same key as the heading. Either would
  # have gone on passing with the button and the heading deleted.
  test 'it sends the reader to the interface on the camera' do
    get '/majestic-endpoints'

    assert_select 'article h2', text: 'Majestic Endpoints'
    assert_select 'article a[href=?]', '/web-interface'
  end

  test 'it names the menu path the camera actually shows' do
    get '/majestic-endpoints'

    # The WebUI menu reads Majestic > Endpoints; "Majestic Endpoints" is only
    # the heading on the page it opens, so telling a reader to look for that
    # string in the menu sends them hunting for something that is not there.
    assert_select 'article', /Majestic\s*→\s*Endpoints/
  end

  test 'it no longer serves a copy of the list' do
    get '/majestic-endpoints'

    # The stale list was recognisable by its example address and its endpoint
    # URLs; nothing that duplicates the device page should come back.
    assert_no_match(/192\.168\.1\.10/, response.body)
    assert_no_match(%r{rtsp://}, response.body)
    assert_no_match(%r{/audio\.opus}, response.body)
  end

  test 'the navigation no longer offers it' do
    get '/'
    assert_response :success
    assert_select 'a[href=?]', '/majestic-endpoints', false,
                  'the menu still points at the page the list moved off'
  end
end
