# frozen_string_literal: true

require 'test_helper'

# The Open Wall used to be walkable. Ids were sequential, so three requests
# found the end of the range, and on 2026-09-21 a fleet of about a thousand
# residential-proxy addresses was fetching /snapshots/<id> in order, one
# request per id, against a robots.txt that has always said Disallow.
#
# An address ban cannot catch a thousand addresses making twenty-five requests
# each, so the capability went instead of the actor (#235). What that means in
# practice is asserted here, and the second test is the one that matters: the
# pictures are the payload, and a page address nobody can guess is worth
# nothing while /wall/<n>/fullhd.jpg still counts from one.
class OpaqueSnapshotIdTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  def upload(mac: '00:11:22:33:44:77')
    snapshot = Snapshot.new(mac_address: mac, ip_address: '203.0.113.9',
                            soc: 'gk7205v300', sensor: 'imx307')
    snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                         content_type: 'image/jpeg')
    snapshot.save!(validate: false)
    snapshot
  end

  test 'a snapshot is addressed by something that cannot be counted to' do
    first = upload(mac: '00:11:22:33:44:01')
    second = upload(mac: '00:11:22:33:44:02')

    assert_match(/\A[0-9a-f]{20}\z/, first.public_id, 'not twenty hex characters')
    assert_not_equal first.public_id, second.public_id
    assert_not_equal first.id.to_s, first.public_id
    assert_equal first.public_id, first.to_param,
                 'every path Rails builds for a snapshot comes from to_param'
  end

  # One hex token in ten thousand is all digits. Both this app and the nginx
  # vhost tell a retired address from a current one by whether it is a number,
  # so such a token would be answered 410 and be unreachable for its whole two
  # days. Asserted on the generator rather than on a sample, because a sample
  # would pass nine thousand nine hundred and ninety nine times out of ten
  # thousand whatever the generator did.
  test 'a generated id is never mistakable for a row id' do
    ids = Array.new(200) { Snapshot.generate_public_id }

    assert_empty ids.grep(/\A[0-9]+\z/), 'an all-digit token would answer 410 for its whole life'
    assert_equal ids.uniq, ids
  end

  test 'the image files are keyed on the same identifier as the page' do
    snapshot = upload
    snapshot.update_columns(variants_generated_at: Time.current)

    url = snapshot.wall_image(:fullhd)

    assert_equal "/wall/#{snapshot.public_id}/fullhd.jpg", url
    assert_no_match(%r{/wall/#{snapshot.id}/}, url,
                    'the pictures are what the crawl was after; a guessable page ' \
                    'address is no use while the images still count from one')
  end

  test 'the retired numeric address is gone, not merely empty' do
    snapshot = upload

    get "/snapshots/#{snapshot.id}"

    assert_response :gone
  end

  test 'the current address still works' do
    snapshot = upload

    get "/snapshots/#{snapshot.public_id}"

    assert_response :success
  end

  # A walk must not be able to tell "no such id" from "wrong shape": both
  # answers have to cost the same and reveal the same, or the id space is
  # searchable again at a slower rate.
  test 'an unknown id of the right shape goes back to the gallery' do
    get "/snapshots/#{'0' * 19}b"

    assert_redirected_to '/open-wall'
  end

  # The nginx location carrying the microcache and the concurrency cap was
  # `snapshots/[0-9]+` until the ids changed. Everything it carries applies
  # only to paths it matches, so an id shape it does not match falls through
  # to the uncached, uncapped catch-all -- reopening the seam that took the
  # site down three times in September.
  test 'the vhost still recognises a snapshot page when it sees one' do
    vhost = Rails.root.join('deploy/nginx/sites-available/org.openipc').read
    cached = vhost[%r{location ~ \^/\(\?:\(\?:ru\|zh\)/\)\?snapshots/(\S+) \{}, 1]

    assert_equal '[0-9]+(?:/|$)', cached, 'the first snapshot location should shed the retired form'

    all = vhost.scan(%r{location ~ \^/\(\?:\(\?:ru\|zh\)/\)\?snapshots/(\S+) \{}).flatten

    assert_includes all, '[0-9a-f]+',
                    'no location matches a current snapshot address, so these pages are ' \
                    'served uncached and uncapped'
  end
end
