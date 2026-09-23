# frozen_string_literal: true

require 'test_helper'

# The Open Wall's images as plain files.
#
# #146 wrote these so a page could link to a path nginx serves without waking
# Ruby. Since 2026-09-23 nothing links to them at all: the files are what
# WallChannel reads and transmits, and they live outside public/ so that
# RAILS_SERVE_STATIC_FILES cannot hand them out behind nginx's back. What is
# guarded here is the storage contract -- keyed on the unguessable id, written
# atomically, purged with the row.
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

  # The ActiveStorage fallback is gone on purpose. It used to return a variant
  # object whenever the files were not there yet, and image_tag turned that
  # into a /rails/active_storage/... URL -- so every frame between upload and
  # ProcessImagesJob was fetchable over HTTP even after #146 moved the files.
  # A frame that has not been processed now simply does not arrive, and the
  # canvas stays empty.
  test 'every variant the wall renders has a path of its own, keyed on the public id' do
    WallImage::VARIANTS.each do |name|
      path = WallImage.path_for(@snapshot.public_id, name).to_s

      assert path.end_with?("#{@snapshot.public_id}/#{name}.jpg"), path
    end
  end

  # The files must not be reachable by Rails' own static middleware, which
  # serves everything under public/ whatever nginx is configured to do.
  test 'the wall tree is not inside public' do
    assert_not WallImage.root.to_s.include?('/public'),
               "wall files under public/ are served by ActionDispatch::Static " \
               'regardless of nginx, which is the door #146 left open'
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
