# frozen_string_literal: true

require 'open3'
require 'test_helper'

# deploy/log-report.sh names the crawlers, and the names are the point: it is
# how the project knows that the LLM crawlers now pull more pages than Yandex
# and Bing, which is an argument about who the site is written for. The census
# it prints replaced a hand-written one in
# ~/reports/analytics-proposal-data/host-pass1-traffic-referrers.sh, the last
# output of that script nothing else produced (#180).
#
# The fixture log is sixteen requests: eleven crawlers that name themselves
# across nine of the report's categories, two that only match the generic
# test, two real browsers, and the snapshot crawler, which presents a browser
# string and must not be in here at all -- it is counted by the beacon in
# deploy/audience-report.sh instead, on a fingerprint rather than a name.
class LogReportTest < ActiveSupport::TestCase
  SCRIPT = Rails.root.join('deploy/log-report.sh')
  FIXTURE = Rails.root.join('test/fixtures/files/crawler-access.log')

  def report
    @report ||= begin
      out, status = Open3.capture2e('bash', SCRIPT.to_s, FIXTURE.to_s)
      assert_predicate status, :success?, "log-report.sh failed:\n#{out}"
      out
    end
  end

  def census
    report[/self-declared crawlers.*?\n\n/m].to_s
  end

  def counted(name)
    census[/^\s+#{Regexp.escape(name)}\s+(\d+)$/, 1]&.to_i
  end

  test 'it names the crawlers that name themselves' do
    assert_equal 2, counted('Googlebot')
    assert_equal 2, counted('Anthropic')
    assert_equal 1, counted('bingbot')
    assert_equal 1, counted('Yandex')
    assert_equal 1, counted('Baiduspider')
    assert_equal 1, counted('OpenAI')
    assert_equal 1, counted('Perplexity')
    assert_equal 1, counted('CN other')
  end

  # Google-InspectionTool is not Googlebot and answering "how much of this is
  # Google" with the wrong one of the two is the mistake the order guards
  # against: the generic test at the end must only catch what the named ones
  # missed.
  test 'a more specific name wins over a more general one' do
    assert_equal 1, counted('Google other'),
                 'Google-InspectionTool is Google, and is not Googlebot'
    assert_equal 2, counted('other, self-declared'),
                 'curl and MJ12bot match nothing named, and must not be silently dropped'
  end

  test 'it says what share of the log the named crawlers are' do
    assert_match(/self-declared crawlers: 13 requests, 81\.2% of the log/, report)
    assert_match(/^  Googlebot\s+2$/, census)
  end

  # The fleet crawling /snapshots presents a current Chrome on macOS. Nothing
  # in a User-Agent gives it away, which is why the beacon catches it on the
  # viewport instead; a census by name that claimed to have found it would be
  # the more dangerous kind of wrong.
  test 'it does not pretend to see the crawler that lies about itself' do
    assert_equal 13, census[/(\d+) requests/, 1].to_i,
                 'two browsers and the snapshot crawler are the three it must not count'
  end
end
