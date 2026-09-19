# frozen_string_literal: true

require 'test_helper'
require 'minitest/mock'

# The job that turns an upload into the four files the wall serves.
#
# It is exercised with the variant source stubbed rather than against real
# libvips, which is not present in the development container and is named in
# CLAUDE.md as a dependency a fresh checkout will not have. What is worth
# pinning here is not that libvips can resize a JPEG -- ActiveStorage's job,
# and unchanged -- but the ordering this job is responsible for: the column
# every view keys on must be set last, and only if all four files landed. Set
# it early and a page links to files that do not exist yet, which is a broken
# image rather than a slow one.
class ProcessImagesJobTest < ActiveJob::TestCase
  # Stands in for snapshot.file: answers .variant(name).processed with
  # something shaped like an ActiveStorage variant, and records what was asked
  # for.
  class FakeFile
    Variant = Struct.new(:image)
    Image = Struct.new(:blob)
    Blob = Struct.new(:key) do
      def download = 'jpeg-bytes'
    end

    attr_reader :requested

    def initialize = @requested = []

    def variant(name)
      @requested << name
      Variant.new(Image.new(Blob.new("key-#{name}")))
    end
  end

  # .processed is what ActiveStorage returns; the fake's variant is already
  # "processed", so it just answers itself.
  module Processed
    def processed = self
  end
  FakeFile::Variant.include(Processed)

  setup do
    @snapshot = Snapshot.new(mac_address: '00:11:22:33:44:55', ip_address: '203.0.113.9')
    @snapshot.save!(validate: false)
    @file = FakeFile.new
    @stored = []
  end

  def run_job
    recorder = ->(id, name, source) { @stored << [id, name, source] }
    @snapshot.stub(:file, @file) do
      WallImage.stub(:store, recorder) do
        block_given? ? yield : ProcessImagesJob.perform_now(@snapshot)
      end
    end
  end

  test 'it writes every variant the wall renders, once each' do
    run_job

    assert_equal WallImage::VARIANTS, @file.requested
    stored_variants = @stored.map { |entry| entry[1] }
    assert_equal WallImage::VARIANTS, stored_variants
    assert_equal [@snapshot.id], @stored.map(&:first).uniq
  end

  test 'it marks the snapshot so the views start linking to the files' do
    assert_nil @snapshot.variants_generated_at

    run_job

    assert_not_nil @snapshot.reload.variants_generated_at
    assert_equal "/wall/#{@snapshot.id}/thumb.jpg", @snapshot.wall_image(:thumb)
  end

  test 'a variant that cannot be written leaves the snapshot unmarked' do
    @snapshot.stub(:file, @file) do
      WallImage.stub(:store, ->(*) { raise Errno::EACCES, 'wall' }) do
        assert_raises(Errno::EACCES) { ProcessImagesJob.perform_now(@snapshot) }
      end
    end

    assert_nil @snapshot.reload.variants_generated_at,
               'a half-written snapshot must keep falling back, not link to files that are missing'
  end

  # The Disk service can say where a variant already is, so the bytes are
  # copied rather than pulled through Ruby -- #download on a 1920x1080 JPEG
  # would build a String for no reason, on a thread serving a request.
  test 'it copies from the service path rather than downloading the bytes' do
    run_job

    sources = @stored.map(&:last)
    assert(sources.all? { |s| s.to_s.include?('key-') },
           'expected on-disk paths derived from the variant keys')
    assert(sources.none? { |s| s == 'jpeg-bytes' },
           'the bytes were pulled through Ruby instead of copied from disk')
  end
end
