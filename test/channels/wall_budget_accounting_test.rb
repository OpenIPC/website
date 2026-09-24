# frozen_string_literal: true

require 'test_helper'

# The budget has to actually bound what it says it bounds.
#
# Lowering FRAME_BUDGET from 3,000 to 1,000 turned it from decoration into a
# number somebody might rely on, and a review immediately found two ways it
# did not hold. Both were invisible while the ceiling was higher than the whole
# wall, because nothing ever reached it.
#
#   * Admission and charging were separate operations -- read the counter, then
#     increment it later inside transmit_frame. Sockets in parallel could each
#     read a figure under the ceiling and all deliver. nginx permits sixteen
#     concurrent sockets per address and one request may name ninety-six
#     frames, so this was worth well over a second budget to anyone who opened
#     connections at the same time, which is what a harvester does.
#
#   * The counter lived in fixed clock-hour buckets, so an address that spent
#     everything at 10:59 was given a fresh allowance at 11:00. The stated
#     hourly bound was really twice that across two minutes.
class WallBudgetAccountingTest < ActionCable::Channel::TestCase
  tests WallChannel

  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b
  ADDRESS = '203.0.113.77'

  setup do
    @cache = Rails.cache
    Rails.cache = ActiveSupport::Cache::MemoryStore.new
    stub_connection connection_id: 'abcdef0123456789', client_ip: ADDRESS
    @id = Snapshot.generate_public_id
    WallImage.store_bytes(@id, :thumb, MINIMAL_JPEG)
  end

  teardown do
    WallImage.purge(@id)
    Rails.cache = @cache
  end

  def grant_for(ids)
    WallGrant.issue(pairs: ids.map { |i| WallGrant.pair(i, 'thumb') })
  end

  def frames_from(transmissions)
    transmissions.filter_map { |t| t['frame'] || t[:frame] }
  end

  # Put the address at the ceiling without writing thousands of files.
  def spend(frames, at: Time.now)
    key = "wall:frames:#{ADDRESS}:#{at.to_i / WallChannel::BUDGET_WINDOW.to_i}"
    Rails.cache.write(key, frames, expires_in: WallChannel::BUDGET_WINDOW)
  end

  test 'a frame is charged before it is read, not after' do
    # One unit below the ceiling: exactly one more frame may be served.
    spend(WallChannel::FRAME_BUDGET - 1)
    subscribe(grant: grant_for([@id]))

    perform :request_frames, 'variant' => 'thumb', 'ids' => [@id]
    assert_equal 1, frames_from(transmissions).size

    perform :request_frames, 'variant' => 'thumb', 'ids' => [@id]
    assert_equal 1, frames_from(transmissions).size,
                 'the ceiling was crossed, so admission is still not charging'
  end

  # There is deliberately NO test here for two sockets racing. A unit test
  # cannot produce genuine concurrency, and one that interleaved two channel
  # objects by hand would pass against either implementation -- it would be
  # theatre. What can be pinned is the property that makes the race impossible:
  # the counter moves BEFORE the frame is read, which is what the test above
  # and the refund test below check.

  # A frame that vanished between the grant and the read must not be charged:
  # the reader never received a picture.
  test 'a reservation for a frame that cannot be read is given back' do
    missing = Snapshot.generate_public_id
    spend(0)
    subscribe(grant: grant_for([missing]))

    perform :request_frames, 'variant' => 'thumb', 'ids' => [missing]

    assert_empty frames_from(transmissions)
    key = "wall:frames:#{ADDRESS}:#{Time.now.to_i / WallChannel::BUDGET_WINDOW.to_i}"
    assert_equal 0, Rails.cache.read(key).to_i,
                 'a frame that was never sent was still charged for'
  end

  # The boundary. Spend everything just before the hour turns, then ask again
  # just after. Fixed buckets hand over a whole fresh allowance; a window that
  # carries the previous bucket hands over only what real time has released.
  #
  # Note the assertion is not "nothing at all". Thirty seconds past the hour a
  # sliding window HAS released about thirty seconds' worth, and refusing that
  # would be wrong -- the point is that it is a trickle rather than a second
  # budget.
  test 'an address cannot double its take across an hour boundary' do
    window = WallChannel::BUDGET_WINDOW.to_i
    wanted = 40
    ids = Array.new(wanted) { Snapshot.generate_public_id }
    ids.each { |i| WallImage.store_bytes(i, :thumb, MINIMAL_JPEG) }

    just_before = Time.at(((Time.now.to_i / window) + 1) * window - 30)
    travel_to just_before do
      spend(WallChannel::FRAME_BUDGET, at: just_before)
    end

    travel_to just_before + 60 do
      subscribe(grant: grant_for(ids))
      perform :request_frames, 'variant' => 'thumb', 'ids' => ids

      served = frames_from(transmissions).size
      assert_operator served, :<, wanted, <<~MESSAGE.chomp
        Crossing the bucket boundary handed this address a fresh allowance
        thirty seconds after it had spent the last one: #{served} of #{wanted}
        frames went out. A budget described as hourly has to mean any hour,
        not the hour the clock happens to be in.
      MESSAGE
    end
  ensure
    ids&.each { |i| WallImage.purge(i) }
  end

  # And the other half: the carry must age out, or an address that spent its
  # budget an hour ago would never be served again.
  test 'the previous window stops counting once it has passed' do
    window = WallChannel::BUDGET_WINDOW.to_i
    long_ago = Time.now - (window * 2)

    travel_to long_ago do
      spend(WallChannel::FRAME_BUDGET, at: long_ago)
    end

    subscribe(grant: grant_for([@id]))
    perform :request_frames, 'variant' => 'thumb', 'ids' => [@id]

    assert_equal 1, frames_from(transmissions).size,
                 'a budget spent two hours ago is still being held against this address'
  end
end
