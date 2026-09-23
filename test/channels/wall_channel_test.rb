# frozen_string_literal: true

require 'test_helper'

# The frame channel, and above all its budget.
#
# The budget is the reason any of this was worth doing. Opaque ids, a
# JavaScript gate, an address block and a rate limit all failed against a
# distributed harvester because each made frames dearer rather than absent, and
# a static file has no session behind it to count against. This channel does.
# If the counting ever stops working, the transport is just a URL that is
# harder to find, and nothing else in the suite would notice.
class WallChannelTest < ActionCable::Channel::TestCase
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  setup do
    stub_connection connection_id: 'abcdef0123456789'
    @ids = Array.new(3) { write_frame }
  end

  teardown do
    @ids.each { |id| WallImage.purge(id) }
  end

  def write_frame
    id = Snapshot.generate_public_id
    WallImage.store_bytes(id, :thumb, MINIMAL_JPEG)
    id
  end

  def frames_from(transmissions)
    transmissions.filter_map { |t| t['frame'] || t[:frame] }
  end

  test 'a subscriber gets the frames it asks for' do
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => @ids

    assert_equal @ids.size, frames_from(transmissions).size
  end

  # The mask is obfuscation, not secrecy -- but it has to round-trip, or the
  # canvas paints noise and the failure looks like a decode bug.
  test 'a frame unmasks back to the bytes on disk' do
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => [@ids.first]

    sent = transmissions.last
    masked = Base64.strict_decode64(sent['frame'] || sent[:frame])
    key = 'abcdef0123456789'.each_char.map(&:ord)
    unmasked = masked.bytes.each_with_index.map { |b, i| b ^ key[i % key.size] }.pack('C*')

    assert_equal MINIMAL_JPEG, unmasked
  end

  test 'a connection is refused once it has taken its budget' do
    subscribe
    budget = WallChannel::FRAME_BUDGET

    # Ask for more distinct frames than any reader could want, in batches the
    # channel accepts, and count what comes back.
    ((budget / WallChannel::MAX_PER_REQUEST) + 2).times do
      ids = Array.new(WallChannel::MAX_PER_REQUEST) { write_frame }
      @ids.concat(ids)
      perform :request_frames, 'variant' => 'thumb', 'ids' => ids
    end

    assert_operator frames_from(transmissions).size, :<=, budget, <<~MESSAGE.chomp
      The channel served past its budget. That budget is the whole mechanism:
      a reader of a twenty-camera wall asks for about twenty frames and the
      harvester took roughly 2,800 distinct ones a day, so this is the line
      that separates them.
    MESSAGE
    assert(transmissions.any? { |t| (t['error'] || t[:error]).present? },
           'the client was cut off without being told why')
  end

  test 'an unknown variant is refused rather than guessed at' do
    subscribe
    perform :request_frames, 'variant' => '../../etc/passwd', 'ids' => @ids

    assert_empty frames_from(transmissions)
    assert_equal 'unknown variant', transmissions.last['error'] || transmissions.last[:error]
  end

  # The ids come from the page and are therefore untrusted. Anything that is
  # not a public_id must never reach the filesystem.
  test 'an id that is not a public id never reaches the disk' do
    subscribe
    perform :request_frames, 'variant' => 'thumb',
                             'ids' => ['../../../etc/passwd', 'nope', "#{@ids.first}/.."]

    assert_empty frames_from(transmissions)
  end

  test 'a request larger than a camera day is refused whole' do
    subscribe
    perform :request_frames, 'variant' => 'thumb',
                             'ids' => Array.new(WallChannel::MAX_PER_REQUEST + 1) { Snapshot.generate_public_id }

    assert_empty frames_from(transmissions)
    assert_equal 'too many frames in one request',
                 transmissions.last['error'] || transmissions.last[:error]
  end

  test 'a frame with no file on disk is simply absent' do
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => [Snapshot.generate_public_id]

    assert_empty frames_from(transmissions)
  end
end
