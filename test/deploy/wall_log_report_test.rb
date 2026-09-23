# frozen_string_literal: true

require 'open3'
require 'test_helper'

# The Open Wall section of deploy/log-report.sh, whose headline figure is an
# invariant rather than a statistic: since #267 no address returns a camera
# frame, so "camera frames served over HTTP" is supposed to read zero and any
# other number means a door reopened.
#
# A number that is meant to be zero is only useful if it can also be non-zero
# for the right reasons and stay zero for the wrong ones, which is what this
# file pins. The detector is the delicate half. `/open-wall/camera/<id>` is the
# HTML page, still served on purpose, painting its frame onto a canvas; only
# the `.jpg` twin ever handed over bytes. A check written as a substring match
# on `open-wall/camera` reports that page as an escaped frame -- which an
# ad-hoc version of this measurement did twice on 2026-09-23, each time costing
# a round of probing to establish that nothing had leaked. The fixture contains
# both spellings, in two locales, so the distinction cannot quietly collapse.
class WallLogReportTest < ActiveSupport::TestCase
  SCRIPT = Rails.root.join('deploy/log-report.sh')
  FIXTURE = Rails.root.join('test/fixtures/files/wall-access.log')

  def report
    @report ||= begin
      out, status = Open3.capture2e('bash', SCRIPT.to_s, FIXTURE.to_s)
      assert_predicate status, :success?, "log-report.sh failed:\n#{out}"
      out
    end
  end

  def wall_section
    report[/^Open Wall$.*?(?=\n\nself-declared|\z)/m].to_s
  end

  test 'it counts the frames that really did leave over HTTP' do
    # The fixture has exactly two: a /wall/ static frame and an original upload
    # through /snapshots/<id>/download, both 200.
    assert_match(/CAMERA FRAMES SERVED OVER HTTP\s+2\b/, wall_section)
  end

  test 'it says plainly that a non-zero reading is a reopened door' do
    assert_match(/meant to be zero/, wall_section,
                 'The count alone does not tell a reader that zero is the ' \
                 'only acceptable value. Without that sentence the number ' \
                 'looks like traffic rather than an alarm.')
  end

  test 'it dates the most recent leak, so history is not read as a regression' do
    # The count cannot distinguish "this happened before the cutover" from
    # "this is happening now", and on the day itself the honest answer was
    # thousands of frames, all of them before 10:17:11. Without a timestamp the
    # headline reads as a live breach every time someone runs it over a log
    # that spans the change.
    assert_match(%r{most recent one\s+23/Sep/2026:10:16:30}, wall_section)
  end

  test 'the HTML camera page is not mistaken for a frame' do
    # Two requests in the fixture, one per locale prefix, both 200 and both
    # ordinary pages. If either is counted the figure above becomes 3 or 4.
    refute_match(/CAMERA FRAMES SERVED OVER HTTP\s+[34]\b/, wall_section,
                 '/open-wall/camera/<id> without an extension is the HTML ' \
                 'page. Counting it invents a frame leak that did not happen.')
  end

  test 'the refusals that enforce the invariant are counted' do
    # Three 410s in the fixture: a /wall/ frame, a download, and the .jpg twin
    # of the camera page -- out of five requests to retired image addresses.
    assert_match(/retired image paths refused\s+3 of 5 requests answered 410/, wall_section)
  end

  test 'the locale-prefixed .jpg twin still counts as a retired address' do
    # /ru/open-wall/camera/<id>.jpg is in the fixture. If the locale prefix
    # were not stripped it would fall out of the detector entirely and the
    # refusal count above would read 2 of 4.
    refute_match(/retired image paths refused\s+2 of 4/, wall_section)
  end

  test 'it reports the channel that replaced those addresses, by status' do
    assert_match(/frame channel\s+3 requests:/, wall_section)
    assert_match(/101=1/, wall_section)
    assert_match(/404=1/, wall_section)
    assert_match(/429=1/, wall_section)
  end

  test 'it explains what a 404 and a 429 on the channel actually mean' do
    # Both are mine to cause and neither is self-evident from the number: a 404
    # is a proxy that dropped the Upgrade header, a 429 is limit_conn counting
    # open tabs. Whoever reads this report next should not have to rediscover
    # that, as this session did.
    assert_match(/Upgrade header/, wall_section)
    assert_match(/counts open tabs/, wall_section)
  end

  test 'it counts the distinct camera ids the site confirmed exist' do
    # Metadata is the part the channel does not hide. Three distinct ids are
    # confirmed by a 200 in the fixture: two snapshot pages, one of them behind
    # a locale prefix, and the original-upload download. A download confirms
    # the camera exists exactly as a page does, which is why this counts any
    # 200 carrying an id rather than pages alone.
    assert_match(/distinct camera ids exposed\s+3\b/, wall_section)
  end
end
