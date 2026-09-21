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
  # public_id, not the row id: the gallery links a snapshot by the identifier
  # that cannot be walked, and a \d+ pattern here would match nothing.
  def ids_on(page)
    get(page ? "/open-wall?page=#{page}" : '/open-wall')
    assert_response :success
    css_select('a[href^="/snapshots/"]').filter_map { |a| a['href'][%r{/snapshots/([0-9a-f]{20})}, 1] }.uniq
  end

  def page_links
    css_select('nav .page-link').map(&:text).map(&:strip)
  end

  # The numbered links only -- Kaminari also renders Previous and Next.
  def numbered_page_links_above(highest)
    numbers = page_links.grep(/\A[0-9]+\z/).map(&:to_i)
    numbers.select { |n| n > highest }
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
    assert_equal Snapshot.latest_per_camera.map(&:public_id).sort, both.sort
  end

  # The count is what draws these. Nineteen cameras at eighteen a page is two,
  # and a count taken over the wrong rows would offer a third.
  test 'the page links offer exactly the pages that exist' do
    get '/open-wall'

    assert_response :success
    assert_includes page_links, '2'
    assert_not_includes page_links, '3', 'a page link to a page with nothing on it'
  end

  # page_title is the <title> and the og:title a link preview shows, so it has
  # to agree with what actually rendered.
  test 'the title names the page that rendered, not the one that was asked for' do
    { nil => 1, '1' => 1, '2' => 2, 'abc' => 1, '0' => 1, '-3' => 1 }.each do |param, expected|
      get(param ? "/open-wall?page=#{param}" : '/open-wall')

      assert_response :success
      # The layout appends the site name, so match the part this sets.
      assert_match(/\AOpen Wall, page #{expected}\b/, css_select('title').first.text,
                   "page=#{param.inspect} rendered page #{expected}")
    end
  end

  test 'a page number that is not a number is the first page' do
    first = ids_on(1)

    ['abc', '', '0', '-3'].each do |param|
      assert_equal first, ids_on(param), "page=#{param.inspect} should be page one"
    end
  end

  # Empty was never the whole assertion. An empty page at a positive offset
  # proves nothing about the total, and inferring one from the offset drew
  # links to ninety-eight pages that do not exist.
  test 'a page past the end renders empty and advertises no page beyond the last' do
    assert_empty ids_on(99)
    assert_not_includes page_links, '3'
    assert_not_includes page_links, '99'
    assert_empty numbered_page_links_above(2), 'links to pages the wall does not have'
  end

  test 'an empty wall is empty rather than inferred' do
    Snapshot.delete_all

    assert_empty ids_on(nil)
    assert_empty numbered_page_links_above(1)
  end

  # The tile prints `snapshot.file.byte_size`, which is an attachment and a
  # blob per tile unless they are preloaded -- 32 of the 33 queries this action
  # used to make. find_by_sql returns records with nothing preloaded, so this
  # is easy to lose by accident and invisible when you do.
  test 'a page costs a bounded number of queries, not one per tile' do
    queries = 0
    counter = ->(_n, _s, _f, _i, payload) { queries += 1 unless payload[:name].to_s.match?(/SCHEMA|TRANSACTION/) }

    ActiveSupport::Notifications.subscribed(counter, 'sql.active_record') { get '/open-wall' }

    assert_response :success
    assert_operator queries, :<, PER_PAGE,
                    "#{queries} queries for #{PER_PAGE} tiles -- the attachment preload has been lost"
  end

  def queries_for(path)
    seen = []
    counter = lambda do |_n, _s, _f, _i, payload|
      seen << payload[:sql].to_s unless payload[:name].to_s.match?(/SCHEMA|TRANSACTION/)
    end
    ActiveSupport::Notifications.subscribed(counter, 'sql.active_record') { get path }
    assert_response :success
    seen
  end

  test 'a short page still reports the right total' do
    Snapshot.where.not(id: Snapshot.latest_per_camera(limit: 3).map(&:id)).delete_all

    assert_equal 3, ids_on(nil).size
    assert_not_includes page_links, '2'
  end
end
