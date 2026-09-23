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
    # The budget lives in Rails.cache and the test environment's is :null_store,
    # which silently forgets everything -- so with the stock store the budget
    # test passes for the wrong reason. Give it a real one.
    @cache = Rails.cache
    Rails.cache = ActiveSupport::Cache::MemoryStore.new

    # client_ip, not request. Handing stub_connection a `request:` made the
    # double answer connection.request publicly while the real
    # ActionCable::Connection::Base keeps it PRIVATE -- so this file was green
    # against a channel that raised NoMethodError on every frame in
    # development. Stub the interface the channel actually uses.
    stub_connection connection_id: 'abcdef0123456789', client_ip: '203.0.113.50'
    @ids = Array.new(3) { write_frame }
  end

  teardown do
    @ids.each { |id| WallImage.purge(id) }
    Rails.cache = @cache
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

  # Spending the budget without generating three thousand files: the counter is
  # the thing under test, not the reading of frames.
  def spend(frames, address = '203.0.113.50')
    key = "wall:frames:#{address}:#{Time.now.to_i / WallChannel::BUDGET_WINDOW}"
    Rails.cache.write(key, frames, expires_in: WallChannel::BUDGET_WINDOW)
  end

  test 'an address is refused once it has taken its hourly budget' do
    spend(WallChannel::FRAME_BUDGET)
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => @ids

    assert_empty frames_from(transmissions)
    assert(transmissions.any? { |t| (t['error'] || t[:error]).present? },
           'the client was cut off without being told why')
  end

  # The budget must survive reconnection, or it is no budget at all: a
  # harvester that hits the ceiling just opens another socket. nginx's
  # limit_conn bounds SIMULTANEOUS sockets, not sequential ones.
  test 'a new connection does not reset the budget' do
    spend(WallChannel::FRAME_BUDGET)

    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => @ids
    assert_empty frames_from(transmissions)

    # A fresh connection, a fresh connection_id, the same address.
    stub_connection connection_id: 'fedcba9876543210', client_ip: '203.0.113.50'
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => @ids

    assert_empty frames_from(transmissions),
                 'reconnecting bought a fresh allowance, which is the whole attack'
  end

  # And the mirror image: a long reading session must never be cut off. The
  # first version of this budget was per connection and permanent, and a reader
  # browsing two camera-days silently stopped seeing pictures.
  test 'a thorough reading session is nowhere near the budget' do
    thorough = 18 + 13 + 96 + 96 # gallery, a camera page, its archive, its slideshow

    assert_operator thorough * 4, :<, WallChannel::FRAME_BUDGET, <<~MESSAGE.chomp
      The budget is close enough to a real visit that a reader could hit it.
      Several people behind one carrier-NAT address share this counter, and a
      reader who stops seeing pictures is the one failure this work must not
      cause.
    MESSAGE
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

  # This is also the mid-purge race, and deliberately the same test.
  #
  # The channel used to check File.exist? and then File.binread, which is a
  # window: PurgeImagesJob destroys snapshots on a nightly cron and
  # Snapshot#purge_wall_images removes the whole directory, so a frame could
  # vanish between the two calls and take the channel action down with an
  # ENOENT. There is no guard now -- it asks for the bytes and rescues -- so a
  # missing file and a file that disappears mid-read are one code path, and one
  # test covers both.
  test 'a frame with no file on disk is simply absent' do
    subscribe
    perform :request_frames, 'variant' => 'thumb', 'ids' => [Snapshot.generate_public_id]

    assert_empty frames_from(transmissions)
  end

  # The reply has to name the variant. One snapshot appears on a page twice at
  # different sizes -- the fullhd hero and the first icon2 tile of its own
  # archive strip are the same id -- and the client keys its waiting canvases
  # on the pair. Without it, whichever reply arrived first painted both, so a
  # hero could be drawn from a 240x135 thumbnail.
  test 'a frame says which variant it is' do
    WallImage.store_bytes(@ids.first, :fullhd, MINIMAL_JPEG)
    subscribe

    perform :request_frames, 'variant' => 'fullhd', 'ids' => [@ids.first]
    perform :request_frames, 'variant' => 'thumb', 'ids' => [@ids.first]

    variants = transmissions.filter_map { |t| t['variant'] || t[:variant] }

    assert_equal %w[fullhd thumb], variants
  end
end
