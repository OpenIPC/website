# frozen_string_literal: true

require 'test_helper'

# The two edge rules added for the Open Wall scraper (#261), and the property
# that makes each of them safe.
#
# The fleet declares nothing. Its three commonest user agents are the same
# string -- Chrome/133.0.0.0 -- on macOS, Windows and Linux at 2,087 / 2,071 /
# 2,022 requests over eight hours, so the crawler map in
# openipc-crawler-block.conf cannot see it and never will. What it cannot
# disguise is that it runs in a datacentre, and that is what the geo block
# keys on.
#
# The danger in an address rule is the opposite of the danger in a user-agent
# rule: it is silent and it hits readers. So the test that matters here is not
# that the ranges are present, it is that every range in the file is one the
# evidence covers -- Microsoft's, and never a residential /16 the same fleet
# also appears in.
class DatacentreBlockTest < ActiveSupport::TestCase
  CONF = Rails.root.join('deploy/nginx/conf.d/openipc-datacentre-block.conf').read.freeze
  RATE = Rails.root.join('deploy/nginx/conf.d/openipc-snapshot-rate.conf').read.freeze
  VHOST = Rails.root.join('deploy/nginx/sites-available/org.openipc').read.freeze

  def blocked_ranges
    CONF.scan(%r{^\s+(\d+\.\d+\.\d+\.\d+/\d+)\s+1;}).flatten
  end

  # The residential /16s the fleet shares with clients that have executed the
  # page beacon, measured over fourteen days of log on 2026-09-22. An address
  # rule that reaches any of these is shedding readers, and the log will show
  # it as nothing at all.
  READER_RANGES = %w[
    104.238 119.123 136.158 144.31 150.241 172.56 177.161 187.188 187.19
    187.190 187.195 43.156 43.157 43.166 45.138 46.150 47.79
  ].freeze

  test 'the block names ranges and every one of them is a /16' do
    assert_operator blocked_ranges.size, :>=, 20, 'the measured fleet spans 22 ranges'
    blocked_ranges.each do |range|
      assert range.end_with?('/16'), "#{range} is not a /16; widen deliberately or not at all"
    end
  end

  test 'no range that carries a real reader is in the block' do
    overlap = blocked_ranges.map { |r| r.split('.').first(2).join('.') } & READER_RANGES

    assert_empty overlap, <<~MESSAGE.chomp
      #{overlap.join(', ')} appears in the block and has carried a client that
      executed the page JavaScript. These are residential ranges the scraper
      also rents; blocking one bans the readers in it and shows up in no log as
      anything but silence. The rate limit is the instrument for a shared
      range, not this file.
    MESSAGE
  end

  test 'the block is applied, and at server level rather than on the gallery' do
    assert_match(/if \(\$openipc_datacentre_client\) \{\s*return 403;/, VHOST)

    gallery = VHOST[%r{location ~ \^/\(\?:\(\?:ru\|zh\)/\)\?snapshots.*?\n    \}}m].to_s
    assert_not_includes gallery, 'openipc_datacentre_client',
                        'the same fleet probes /.git/config, so this is not a gallery rule'
  end

  # The zone has to be declared before it is used and spelled the same in both
  # places; nginx fails to start on a mismatch, which is a bad way to find out.
  test 'the snapshot rate zone is declared and used under one name' do
    declared = RATE[/limit_req_zone\s+\S+\s+zone=(\w+):/, 1]
    assert_equal 'snapshot_pages', declared

    assert_match(/limit_req zone=#{declared} burst=(\d+) nodelay;/, VHOST)
  end

  # p99 and the maximum for a real reader were both 46 snapshot pages in a
  # minute. burst has to clear that on its own, or the busiest genuine visitor
  # on the wall is the first person the rule answers 429.
  test 'the burst clears the busiest reader ever measured' do
    burst = VHOST[/limit_req zone=snapshot_pages burst=(\d+) nodelay;/, 1].to_i

    assert_operator burst, :>=, 46, <<~MESSAGE.chomp
      burst=#{burst} is below the 46 pages a minute a real reader reached on
      2026-09-22. The fleet sits at 14-16 a minute per address, so tightening
      this does not reach it and does reach them.
    MESSAGE
  end

  test 'the rate limit is on the snapshot pages and keyed on the reader' do
    assert_match(/limit_req_zone\s+\$binary_remote_addr/, RATE,
                 'keyed on the mirror instead would pool every visitor behind openipc.ru onto one key')

    gallery = VHOST[%r{location ~ \^/\(\?:\(\?:ru\|zh\)/\)\?snapshots/\[0-9a-f\].*?\n    \}}m].to_s
    assert_includes gallery, 'limit_req zone=snapshot_pages'
  end
end
