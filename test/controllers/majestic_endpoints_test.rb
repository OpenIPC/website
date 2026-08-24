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

  test 'it sends the reader to the interface on the camera' do
    get '/majestic-endpoints'
    assert_select 'a[href=?]', '/web-interface'
    assert_select 'article' do
      assert_match(/Majestic Endpoints/, response.body)
    end
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
