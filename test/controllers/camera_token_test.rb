# frozen_string_literal: true

require 'test_helper'

# Each camera's page used to live at its MAC address written as a decimal
# integer: /open-wall/camera/261946086576566 reads back as ee:3d:13:70:d1:b6,
# and the first three octets name the manufacturer.
#
# The intent to withhold the address was already in the code -- snapshots/show
# prints it only for a signed-in admin -- but the link beside it handed the
# same value to everyone, and to anything crawling the wall. The address is
# hardware that outlives the picture, which is retained for two days.
class CameraTokenTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b
  MAC = '00:11:22:33:44:55'

  setup do
    @snapshot = Snapshot.new(mac_address: MAC, ip_address: '203.0.113.9',
                             soc: 'gk7205v300', sensor: 'imx307')
    @snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 's.jpg', content_type: 'image/jpeg')
    @snapshot.save!(validate: false)
  end

  test 'the camera link does not contain the address in any form' do
    get "/snapshots/#{@snapshot.public_id}"

    assert_response :success
    links = css_select('a[href*="/open-wall/camera/"]').map { |a| a['href'] }
    refute_empty links, 'the per-camera link is gone entirely'

    decimal = MAC.delete(':').to_i(16).to_s
    links.each do |href|
      assert_not_includes href, decimal, "#{href} is the MAC as a decimal integer"
      assert_not_includes href.downcase, MAC.delete(':').downcase, "#{href} is the MAC in hex"
    end
  end

  test 'the token resolves back to that camera' do
    get "/open-wall/camera/#{@snapshot.camera_token}"

    assert_response :success
  end

  # The old form has to stop working, not merely stop being linked. Left
  # answering, the addresses would still be enumerable, which is most of what
  # was wrong with it.
  test 'the old decimal address no longer resolves' do
    get "/open-wall/camera/#{MAC.delete(':').to_i(16)}"

    assert_redirected_to '/open-wall'
  end

  test 'a wrong token is refused without saying what was asked for' do
    get '/open-wall/camera/deadbeefdeadbeef'

    assert_redirected_to '/open-wall'
    assert_not_includes flash[:alert].to_s, 'deadbeef',
                        'the message repeats the probe back to whoever sent it'
  end

  # Not reversible and not guessable from the address itself: the token is an
  # HMAC under the server's key, so knowing a MAC does not produce its token.
  test 'the token is not derivable from the address alone' do
    assert_not_equal MAC.delete(':').to_i(16).to_s, @snapshot.camera_token
    assert_no_match(/001122334455/, @snapshot.camera_token)
    assert_match(/\A[0-9a-f]{16}\z/, @snapshot.camera_token)
  end

  # MAC_ADDRESS_FORMAT accepts either case and either separator, nothing
  # normalises on write, and the column's collation folds case but not "-"
  # against ":". So one camera can land in the table under three spellings,
  # and hashing the raw string would give it three tokens, two of which
  # resolve to nothing.
  test 'one camera is one token however its address was spelled' do
    %w[AA:BB:CC:DD:EE:FF aa-bb-cc-dd-ee-ff Aa:Bb:Cc:Dd:Ee:Ff].each do |spelling|
      assert_equal Snapshot.camera_token_for('aa:bb:cc:dd:ee:ff'),
                   Snapshot.camera_token_for(spelling),
                   "#{spelling} hashes to a different camera"
    end
  end

  test 'a camera that uploaded under two spellings is found by its token' do
    other = Snapshot.new(mac_address: MAC.downcase.tr(':', '-'), ip_address: '203.0.113.11',
                         soc: 'x', sensor: 'y')
    other.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 's.jpg', content_type: 'image/jpeg')
    other.save!(validate: false)

    assert_equal @snapshot.camera_token, other.camera_token

    get "/open-wall/camera/#{other.camera_token}"

    assert_response :success
  end

  test 'the same camera keeps the same token' do
    second = Snapshot.new(mac_address: MAC, ip_address: '203.0.113.10', soc: 'x', sensor: 'y')
    second.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 's.jpg', content_type: 'image/jpeg')
    second.save!(validate: false)

    assert_equal @snapshot.camera_token, second.camera_token,
                 'a camera would get a new address on every upload'
  end
end
