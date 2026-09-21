# frozen_string_literal: true

require 'test_helper'

# The Open Wall's images as plain files.
#
# What this guards is the reason the whole change exists: a page must be able
# to link to a path nginx can serve without waking Ruby, and must fall back to
# the ActiveStorage variant whenever those files are not there yet -- a row
# uploaded seconds ago, a row predating the backfill, or a job lost to the
# :async adapter on a restart. Getting the fallback wrong is worse than not
# doing this at all: it shows broken images rather than slow ones.
class WallImageTest < ActiveSupport::TestCase
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  setup do
    @snapshot = Snapshot.new(mac_address: '00:11:22:33:44:55', ip_address: '203.0.113.9')
    @snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'x.jpg', content_type: 'image/jpeg')
    @snapshot.save!(validate: false)
  end

  teardown do
    WallImage.purge(@snapshot.public_id)
  end

  test 'a snapshot with no generated variants falls back to ActiveStorage' do
    assert_nil @snapshot.variants_generated_at

    image = @snapshot.wall_image(:thumb)

    assert_kind_of ActiveStorage::VariantWithRecord, image,
                   'until the files exist the page must keep using the variant it always used'
  end

  test 'a snapshot with generated variants links to the plain file' do
    @snapshot.update_column(:variants_generated_at, Time.current)

    assert_equal "/wall/#{@snapshot.public_id}/thumb.jpg", @snapshot.wall_image(:thumb)
  end

  test 'every variant the wall renders has a path of its own' do
    @snapshot.update_column(:variants_generated_at, Time.current)

    WallImage::VARIANTS.each do |name|
      assert_equal "/wall/#{@snapshot.public_id}/#{name}.jpg", @snapshot.wall_image(name)
    end
  end

  # The bytes have to arrive complete or not at all: nginx may be serving this
  # exact path to somebody while it is replaced, and a truncated JPEG is worse
  # than a missing one. This is the trap that once had Firmware#publish serving
  # partial images.
  test 'storing writes the file atomically and leaves no temporary behind' do
    source = Tempfile.new(['src', '.jpg'])
    source.binmode
    source.write(MINIMAL_JPEG)
    source.flush

    WallImage.store(@snapshot.public_id, :thumb, source.path)

    path = WallImage.path_for(@snapshot.public_id, :thumb)
    assert_path_exists path
    assert_equal MINIMAL_JPEG, File.binread(path)
    assert_equal [path.to_s], Dir.glob(WallImage.dir_for(@snapshot.public_id).join('*')).sort,
                 'a leftover temporary file would be served as garbage or swept as an orphan'
  ensure
    source&.close!
  end

  test 'the stored file is readable by the web server, not just by us' do
    source = Tempfile.new(['src', '.jpg'])
    source.binmode
    source.write(MINIMAL_JPEG)
    source.flush
    File.chmod(0o600, source.path)

    WallImage.store(@snapshot.public_id, :icon, source.path)

    mode = File.stat(WallImage.path_for(@snapshot.public_id, :icon)).mode & 0o777
    assert_equal 0o644, mode, 'nginx runs as another user and would answer 403'
  ensure
    source&.close!
  end

  # Nothing else removes these: they are not ActiveStorage's, so neither its
  # purge nor storage:reap can see them.
  test 'destroying a snapshot takes its wall directory with it' do
    source = Tempfile.new(['src', '.jpg'])
    source.binmode
    source.write(MINIMAL_JPEG)
    source.flush
    WallImage.store(@snapshot.public_id, :thumb, source.path)
    dir = WallImage.dir_for(@snapshot.public_id)
    assert_path_exists dir

    @snapshot.destroy

    refute File.exist?(dir), 'a wall directory with no row behind it is unreachable and never freed'
  ensure
    source&.close!
  end

  test 'purging a snapshot that never had wall files is not an error' do
    assert_nothing_raised { WallImage.purge(@snapshot.public_id) }
  end
end
