# frozen_string_literal: true

require 'test_helper'

# The Open Wall's greatest-n-per-group, which now has to answer two questions
# consistently: which rows are on this page, and how many pages there are. A
# count taken over different rows than the page would paginate to pages that do
# not exist, or hide ones that do -- so both come from one SQL fragment and
# these tests are what hold them together (#152).
class LatestPerCameraTest < ActiveSupport::TestCase
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  setup do
    Snapshot.delete_all
    @macs = (1..5).map { |i| format('00:11:22:33:44:%02d', i) }
  end

  # Two per camera, so anything that forgets the grouping returns ten.
  def upload(mac, created_at)
    s = Snapshot.new(mac_address: mac, ip_address: '203.0.113.9', soc: 'gk7205v300', sensor: 'imx307')
    s.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg', content_type: 'image/jpeg')
    s.save!(validate: false)
    s.update_columns(created_at: created_at)
    s
  end

  def fill(count: 5)
    @macs.first(count).each_with_index do |mac, i|
      upload(mac, (i + 10).minutes.ago)
      upload(mac, (i + 1).minutes.ago)
    end
  end

  test 'it returns the newest row per camera, not every row' do
    fill

    assert_equal 5, Snapshot.latest_per_camera.size
    assert_equal 10, Snapshot.count
  end

  test 'the count agrees with the number of rows the page query can return' do
    fill

    assert_equal Snapshot.latest_per_camera.size, Snapshot.latest_per_camera_count
  end

  test 'the count builds no row objects' do
    fill

    assert_kind_of Integer, Snapshot.latest_per_camera_count
  end

  test 'a limit cuts the result without changing the order' do
    fill
    all = Snapshot.latest_per_camera

    assert_equal all.first(2).map(&:id), Snapshot.latest_per_camera(limit: 2).map(&:id)
  end

  # The bug this method was changed to fix: page 2 showing page 1.
  test 'an offset moves the window' do
    fill
    all = Snapshot.latest_per_camera.map(&:id)

    assert_equal all[2, 2], Snapshot.latest_per_camera(limit: 2, offset: 2).map(&:id)
    assert_equal all[4, 2], Snapshot.latest_per_camera(limit: 2, offset: 4).map(&:id)
  end

  test 'the pages together cover the whole result exactly once' do
    fill
    per = 2
    pages = (0...Snapshot.latest_per_camera_count).step(per).map do |offset|
      Snapshot.latest_per_camera(limit: per, offset: offset).map(&:id)
    end

    assert_equal Snapshot.latest_per_camera.map(&:id), pages.flatten
    assert_equal pages.flatten.uniq, pages.flatten, 'a row appearing on two pages'
  end

  test 'an offset past the end is empty rather than wrong' do
    fill

    assert_empty Snapshot.latest_per_camera(limit: 18, offset: 1_000)
  end

  # MySQL has no OFFSET without a LIMIT. Dropping it silently would serve page
  # one under every page number, which is the failure being fixed.
  test 'an offset without a limit is refused rather than ignored' do
    assert_raises(ArgumentError) { Snapshot.latest_per_camera(offset: 5) }
  end

  test 'rows older than a day are outside both the page and the count' do
    fill
    stale = upload('00:11:22:33:44:99', 25.hours.ago)

    assert_not_includes Snapshot.latest_per_camera.map(&:id), stale.id
    assert_equal 5, Snapshot.latest_per_camera_count
  end
end
