# frozen_string_literal: true

require 'test_helper'

# The gallery used to load the whole 24-hour result set on every view and let
# Kaminari throw away all but eighteen rows. It now asks for one page and for a
# count, which is two chances to disagree: the page links come from the count,
# the tiles come from the page, and nothing in the old test suite would have
# noticed them drifting apart (#152).
class OpenWallPaginationTest < ActionDispatch::IntegrationTest
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b
  PER_PAGE = SnapshotsController::PER_PAGE

  setup do
    Snapshot.delete_all
    # One more than a page, so there is a second page with exactly one tile on
    # it -- the boundary where an off-by-one in the count shows up.
    (PER_PAGE + 1).times do |i|
      mac = format('00:11:22:33:%<high>02x:%<low>02x', high: i / 256, low: i % 256)
      s = Snapshot.new(mac_address: mac,
                       ip_address: '203.0.113.9', soc: 'gk7205v300', sensor: 'imx307')
      s.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                    content_type: 'image/jpeg')
      s.save!(validate: false)
      s.update_columns(created_at: i.minutes.ago)
    end
  end

  # Read the ids out of the rendered page rather than out of the controller.
  # `assigns` needs the rails-controller-testing gem, and the page is the thing
  # a visitor actually gets -- if the tiles and the page links disagree, it
  # shows up here and not in an instance variable.
  def ids_on(page)
    get(page ? "/open-wall?page=#{page}" : '/open-wall')
    assert_response :success
    css_select('a[href^="/snapshots/"]').map { |a| a['href'][%r{/snapshots/(\d+)}, 1].to_i }.uniq
  end

  def page_links
    css_select('nav .page-link').map(&:text).map(&:strip)
  end

  test 'the first page holds one page of cameras' do
    assert_equal PER_PAGE, ids_on(nil).size
  end

  test 'the second page holds the remainder and none of the first' do
    first = ids_on(1)
    second = ids_on(2)

    assert_equal 1, second.size
    assert_empty first & second, 'a camera appearing on both pages'
  end

  test 'the pages together cover every camera exactly once' do
    both = ids_on(1) + ids_on(2)

    assert_equal PER_PAGE + 1, both.size
    assert_equal both.uniq, both
    assert_equal Snapshot.latest_per_camera.map(&:id).sort, both.sort
  end

  # The count is what draws these. Nineteen cameras at eighteen a page is two,
  # and a count taken over the wrong rows would offer a third.
  test 'the page links offer exactly the pages that exist' do
    get '/open-wall'

    assert_response :success
    assert_includes page_links, '2'
    assert_not_includes page_links, '3', 'a page link to a page with nothing on it'
  end

  test 'a page number that is not a number is the first page' do
    first = ids_on(1)

    ['abc', '', '0', '-3'].each do |param|
      assert_equal first, ids_on(param), "page=#{param.inspect} should be page one"
    end
  end

  test 'a page past the end renders empty rather than failing' do
    assert_empty ids_on(99)
  end
end
