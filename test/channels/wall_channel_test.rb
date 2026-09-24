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

  ADDRESS = '203.0.113.50'

  def grant_for(ids, variants: WallChannel::VARIANTS)
    WallGrant.issue(pairs: ids.product(Array(variants)).map { |i, v| WallGrant.pair(i, v) })
  end

  # Every test below subscribes through a grant, because a subscription without
  # one is refused outright and would make the whole file fail for one reason.
  # The grant names the frames the test wrote, which is what a page render
  # would have produced.
  def subscribe_granted(ids: @ids, **options)
    subscribe(grant: grant_for(ids, **options))
  end

  test 'a subscriber gets the frames it asks for' do
    subscribe_granted
    perform :request_frames, 'variant' => 'thumb', 'ids' => @ids

    assert_equal @ids.size, frames_from(transmissions).size
  end

  # The mask is obfuscation, not secrecy -- but it has to round-trip, or the
  # canvas paints noise and the failure looks like a decode bug.
  #
  # Deliberately on a frame LARGER than MASK_BYTES. The mask covers only the
  # head, so a test on a 22-byte image never reaches the boundary and would
  # pass whatever the constant said -- including if the server and
  # src/wall.js disagreed about it, which is the failure this guards.
  test 'a frame unmasks back to the bytes on disk, across the mask boundary' do
    big = MINIMAL_JPEG + SecureRandom.bytes(WallChannel::MASK_BYTES * 2)
    id = Snapshot.generate_public_id
    @ids << id
    WallImage.store_bytes(id, :thumb, big)

    subscribe_granted
    perform :request_frames, 'variant' => 'thumb', 'ids' => [id]

    sent = transmissions.last
    masked = Base64.strict_decode64(sent['frame'] || sent[:frame])
    key = 'abcdef0123456789'.each_char.map(&:ord)
    head = masked.byteslice(0, WallChannel::MASK_BYTES)
                 .bytes.each_with_index.map { |b, i| b ^ key[i % key.size] }.pack('C*')

    assert_equal big, head + masked.byteslice(WallChannel::MASK_BYTES..).to_s
  end

  # The two constants, compared directly.
  #
  # The round-trip test above derives its boundary from WallChannel::MASK_BYTES
  # and therefore checks the server against itself -- so if src/wall.js and the
  # channel ever disagreed, both tests would pass and every frame would paint
  # as noise from the boundary onward. The PR that added them claimed they
  # "hold the two constants together". They did not. This does.
  test 'the client masks exactly as many bytes as the server' do
    js = Rails.root.join('app/javascript/src/wall.js').read
    declared = js[/^const MASK_BYTES = (\d+)/, 1]

    assert declared, 'src/wall.js no longer declares MASK_BYTES; the client cannot unmask'
    assert_equal WallChannel::MASK_BYTES, declared.to_i, <<~MESSAGE.chomp
      The client unmasks #{declared} bytes and the server masks
      #{WallChannel::MASK_BYTES}. Every frame will paint as noise from
      whichever boundary is lower, and no other test in this file notices,
      because they all take the server's constant as the truth.
    MESSAGE
  end

  # And the mask must actually be applied where it claims: the head altered,
  # the tail untouched. Without this, MASK_BYTES = 0 would pass the round trip
  # above while sending every frame in the clear.
  test 'the head is masked and the tail is not' do
    big = MINIMAL_JPEG + SecureRandom.bytes(WallChannel::MASK_BYTES * 2)
    id = Snapshot.generate_public_id
    @ids << id
    WallImage.store_bytes(id, :thumb, big)

    subscribe_granted
    perform :request_frames, 'variant' => 'thumb', 'ids' => [id]

    masked = Base64.strict_decode64(transmissions.last['frame'] || transmissions.last[:frame])

    assert_not_equal big.byteslice(0, WallChannel::MASK_BYTES),
                     masked.byteslice(0, WallChannel::MASK_BYTES),
                     'the head went out unmasked, so the frame is a renderable JPEG on the wire'
    assert_equal big.byteslice(WallChannel::MASK_BYTES..),
                 masked.byteslice(WallChannel::MASK_BYTES..),
                 'the tail was masked, which costs time and buys nothing'
  end

  # Spending the budget without generating three thousand files: the counter is
  # the thing under test, not the reading of frames.
  def spend(frames, address = '203.0.113.50')
    key = "wall:frames:#{address}:#{Time.now.to_i / WallChannel::BUDGET_WINDOW}"
    Rails.cache.write(key, frames, expires_in: WallChannel::BUDGET_WINDOW)
  end

  test 'an address is refused once it has taken its hourly budget' do
    spend(WallChannel::FRAME_BUDGET)
    subscribe_granted
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

    subscribe_granted
    perform :request_frames, 'variant' => 'thumb', 'ids' => @ids
    assert_empty frames_from(transmissions)

    # A fresh connection, a fresh connection_id, the same address.
    stub_connection connection_id: 'fedcba9876543210', client_ip: '203.0.113.50'
    subscribe_granted
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
    subscribe_granted
    perform :request_frames, 'variant' => '../../etc/passwd', 'ids' => @ids

    assert_empty frames_from(transmissions)
    assert_equal 'unknown variant', transmissions.last['error'] || transmissions.last[:error]
  end

  # The ids come from the page and are therefore untrusted. Anything that is
  # not a public_id must never reach the filesystem.
  test 'an id that is not a public id never reaches the disk' do
    subscribe_granted
    perform :request_frames, 'variant' => 'thumb',
                             'ids' => ['../../../etc/passwd', 'nope', "#{@ids.first}/.."]

    assert_empty frames_from(transmissions)
  end

  test 'a request larger than a camera day is refused whole' do
    subscribe_granted
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
    missing = Snapshot.generate_public_id
    # Granted on purpose. Without that the id is filtered by the grant before
    # it ever reaches read_frame, and this test would pass while proving
    # nothing about the ENOENT rescue it exists for.
    subscribe_granted(ids: [missing])
    perform :request_frames, 'variant' => 'thumb', 'ids' => [missing]

    assert_empty frames_from(transmissions)
  end

  # The reply has to name the variant. One snapshot appears on a page twice at
  # different sizes -- the fullhd hero and the first icon2 tile of its own
  # archive strip are the same id -- and the client keys its waiting canvases
  # on the pair. Without it, whichever reply arrived first painted both, so a
  # hero could be drawn from a 240x135 thumbnail.
  test 'a frame says which variant it is' do
    WallImage.store_bytes(@ids.first, :fullhd, MINIMAL_JPEG)
    subscribe_granted

    perform :request_frames, 'variant' => 'fullhd', 'ids' => [@ids.first]
    perform :request_frames, 'variant' => 'thumb', 'ids' => [@ids.first]

    variants = transmissions.filter_map { |t| t['variant'] || t[:variant] }

    assert_equal %w[fullhd thumb], variants
  end
end
