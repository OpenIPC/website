# frozen_string_literal: true

require 'rake'
require 'test_helper'

# `wall:prune` deletes image directories that no snapshot claims, and it
# decides that by matching directory names against the database. While the
# directories were named by row id it asked `where(id: keys)`. Keying them on
# public_id instead (#235) left that query intact and lethal: MySQL casts a
# hex token to 0, no row has id 0, so every live directory looks orphaned and
# one run empties the wall.
#
# Nothing else would have caught it. The task is not on any request path, it
# runs from cron, and its output is a count that looks the same whether it
# removed three directories or three thousand.
class WallRakeTest < ActiveSupport::TestCase
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  setup do
    Rails.application.load_tasks unless Rake::Task.task_defined?('wall:prune')
    %w[wall:prune wall:rekey].each { |t| Rake::Task[t].reenable }

    @snapshot = Snapshot.new(mac_address: '00:11:22:33:44:88', ip_address: '203.0.113.9',
                             soc: 'gk7205v300', sensor: 'imx307')
    @snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                          content_type: 'image/jpeg')
    @snapshot.save!(validate: false)
  end

  teardown do
    FileUtils.rm_rf(WallImage.root)
  end

  def store_variant(key)
    Tempfile.create(['wall', '.jpg']) do |f|
      f.binmode
      f.write(MINIMAL_JPEG)
      f.flush
      WallImage.store(key, :thumb, f.path)
    end
  end

  test 'prune keeps the directories a snapshot still claims' do
    store_variant(@snapshot.public_id)
    store_variant('deadbeefdeadbeefdead')

    capture_io { Rake::Task['wall:prune'].invoke }

    assert_path_exists WallImage.dir_for(@snapshot.public_id),
                       'prune deleted a live snapshot\'s images'
    assert_not File.exist?(WallImage.dir_for('deadbeefdeadbeefdead')),
               'prune left a directory no row claims'
  end

  # The one-off that carries the images across the change. Everything on disk
  # was named by row id, and would have 404d behind the new URLs until the
  # two-day retention cleared it -- a broken wall, and two more days of a
  # walkable one.
  test 'rekey renames a row-id directory to the public one' do
    store_variant(@snapshot.id.to_s)

    capture_io { Rake::Task['wall:rekey'].invoke }

    assert_path_exists WallImage.dir_for(@snapshot.public_id)
    assert_not File.exist?(WallImage.dir_for(@snapshot.id.to_s)),
               'the numeric directory is still there, and still walkable'
  end

  test 'rekey leaves a directory that has already been carried across' do
    store_variant(@snapshot.public_id)

    capture_io { Rake::Task['wall:rekey'].invoke }

    assert_path_exists WallImage.path_for(@snapshot.public_id, :thumb),
                       'a second run must not disturb what the first one moved'
  end
end
