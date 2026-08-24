# frozen_string_literal: true

require 'test_helper'

# The index is one file written by an hourly cron, and every firmware download
# on the site is answered from it.
class ReleaseIndexTest < ActiveSupport::TestCase
  def setup
    @root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = @root
    ReleaseIndex.reset!
  end

  def teardown
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(@root)
  end

  def write(generated_at:, assets: {})
    File.write(File.join(@root, '.index.json'),
               JSON.generate('generated_at' => generated_at, 'aliases' => {}, 'assets' => assets))
    ReleaseIndex.reset!
  end

  def logged
    io = StringIO.new
    previous = Rails.logger
    Rails.logger = ActiveSupport::Logger.new(io)
    yield
    io.string
  ensure
    Rails.logger = previous
  end

  test 'an index frozen by a cron that stopped running is complained about' do
    # It looks exactly like a current one otherwise: every name it lists still
    # resolves, and nothing new ever appears.
    write(generated_at: 30.hours.ago.utc.iso8601)

    assert_match(/is the publisher still running/, logged { ReleaseIndex.current })
  end

  test 'a current index says nothing' do
    write(generated_at: 20.minutes.ago.utc.iso8601)

    assert_no_match(/publisher/, logged { ReleaseIndex.current })
  end

  test 'the complaint is throttled rather than repeated every request' do
    write(generated_at: 30.hours.ago.utc.iso8601)

    output = logged { 5.times { ReleaseIndex.current } }
    assert_equal 1, output.scan(/is the publisher still running/).size
  end

  test 'an unreadable generated_at is reported, not raised' do
    write(generated_at: 'the day before yesterday')

    assert_match(/is not a timestamp/, logged { ReleaseIndex.current })
  end

  test 'a missing generated_at is simply not checked' do
    File.write(File.join(@root, '.index.json'), JSON.generate('assets' => {}))
    ReleaseIndex.reset!

    assert_no_match(/publisher|timestamp/, logged { ReleaseIndex.current })
  end
end
