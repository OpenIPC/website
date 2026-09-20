# frozen_string_literal: true

require 'test_helper'

class FirmwareBuildTest < ActiveSupport::TestCase
  IP = '203.0.113.7'

  test 'an address under the limit is not over it' do
    (FirmwareBuild::LIMIT - 1).times { FirmwareBuild.record(IP) }

    assert_not FirmwareBuild.over_limit?(IP)
  end

  test 'an address that has reached the limit is over it' do
    FirmwareBuild::LIMIT.times { FirmwareBuild.record(IP) }

    assert FirmwareBuild.over_limit?(IP)
  end

  test 'builds older than the window do not count' do
    FirmwareBuild::LIMIT.times do
      FirmwareBuild.create!(ip_address: IP, created_at: FirmwareBuild::WINDOW.ago - 1.second)
    end

    assert_not FirmwareBuild.over_limit?(IP)
  end

  # The whole point of the limit is that it is per address. A shared limit
  # would mean one enumerator could stop everyone else downloading firmware,
  # which is the failure this replaces rather than repeats.
  test 'one address at the limit does not put another address over it' do
    FirmwareBuild::LIMIT.times { FirmwareBuild.record(IP) }

    assert_not FirmwareBuild.over_limit?('198.51.100.4')
  end

  test 'a blank address is never over the limit and records nothing' do
    assert_no_difference -> { FirmwareBuild.count } do
      FirmwareBuild.record(nil)
      FirmwareBuild.record('')
    end
    assert_not FirmwareBuild.over_limit?(nil)
  end

  test 'recording prunes rows past the retention window' do
    old = FirmwareBuild.create!(ip_address: IP, created_at: FirmwareBuild::RETENTION.ago - 1.minute)
    recent = FirmwareBuild.create!(ip_address: IP, created_at: 30.minutes.ago)

    FirmwareBuild.record(IP)

    assert_not FirmwareBuild.exists?(old.id)
    assert FirmwareBuild.exists?(recent.id)
  end

  # A limiter that raises is worse than a limiter that leaks: the request it
  # kills has already done the expensive work. Same reasoning as Download
  # .record, and the column is limit: 45 so an over-long value is a real way
  # to reach the rescue.
  test 'a value the column cannot hold is logged rather than raised' do
    assert_nothing_raised do
      assert_nil FirmwareBuild.record('9' * 200)
    end
    assert_equal 0, FirmwareBuild.count
  end
end
