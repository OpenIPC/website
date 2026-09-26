# frozen_string_literal: true

require 'test_helper'
require 'open3'
require 'tmpdir'

# deploy/shadow-report.sh is the gate before cameras are moved to Go (#294):
# every mirrored upload must have been seen, decided and stored the same way.
# Run against logs and stand-in database clients, so what it accepts and what
# it refuses is pinned without a host.
class ShadowReportTest < ActiveSupport::TestCase
  SCRIPT = Rails.root.join('deploy/shadow-report.sh').to_s

  DECISIONS = <<~LOG
    2026-09-27T10:00:00+00:00 req0001 201 /snapshots/r0000000000000000001
    2026-09-27T10:30:00+00:00 req0002 201 /snapshots/r0000000000000000002
    2026-09-27T10:31:00+00:00 req0003 429 -
    2026-09-27T10:32:00+00:00 req0004 415 -
    2026-09-27T10:33:00+00:00 req0005 - -
    2026-09-27T10:33:00+00:00 req0006 - -
    2026-09-27T10:34:00+00:00 req0007 429 -
  LOG

  def shadow_line(id, status, location)
    head = '{"time":"2026-09-27T10:30:00Z","level":"INFO","msg":"upload_decision","role":"web",'
    %(#{head}"request_id":"#{id}","status":#{status},"location":"#{location}"}\n)
  end

  ROW = "\t02:c0:f0:00:00:02\t1.2.3.4\tNULL\tfw\tNULL\tNULL\timx335\tgk7205v300\t55.1\tmajestic\t%s\timage/jpeg\t12288"

  # mysql answers with Rails' row; runuser (psql as postgres) with the shadow's.
  def stand_ins(dir, go_uptime)
    FileUtils.mkdir_p("#{dir}/bin")
    File.write("#{dir}/bin/mysql", "#!/bin/sh\nprintf 'r0000000000000000002#{format(ROW, '3 days')}\\n'\n")
    File.write("#{dir}/bin/runuser", "#!/bin/sh\nprintf 'g0000000000000000002#{format(ROW, go_uptime)}\\n'\n")
    FileUtils.chmod(0o755, Dir["#{dir}/bin/*"])
  end

  def report(shadow_lines, go_uptime: '3 days')
    Dir.mktmpdir do |dir|
      File.write("#{dir}/decisions.log", DECISIONS)
      File.write("#{dir}/shadow.log", shadow_lines.join)
      stand_ins(dir, go_uptime)
      env = { 'PATH' => "#{dir}/bin:#{ENV.fetch('PATH')}", 'DECISIONS_LOG' => "#{dir}/decisions.log",
              'SHADOW_LOG' => "#{dir}/shadow.log" }
      out, status = Open3.capture2e(env, 'bash', SCRIPT)
      [out, status.success?]
    end
  end

  def all_seen
    [shadow_line('req0001', 201, '/snapshots/g0000000000000000001'),
     shadow_line('req0002', 201, '/snapshots/g0000000000000000002'),
     shadow_line('req0003', 429, ''), shadow_line('req0004', 415, ''), shadow_line('req0007', 429, '')]
  end

  test 'every upload seen, decided and stored the same way passes, after the warm-up' do
    out, ok = report(all_seen)

    assert ok, out
    assert_match(/compared 4 uploads/, out, 'the warm-up, and what no backend saw, are skipped')
    assert_match(/1 frames both stored; 0 row lines differ/, out)
  end

  test 'a different decision fails' do
    lines = all_seen
    lines[2] = shadow_line('req0003', 201, '/snapshots/g0000000000000000003')
    out, ok = report(lines)

    refute ok, out
    assert_match(/DECISIONS DISAGREE/, out)
  end

  test 'the same decision with a different stored row fails' do
    out, ok = report(all_seen, go_uptime: '4 days')

    refute ok, out
    assert_match(/STORED ROWS DIFFER/, out)
  end

  # nginx logged 499 for it at the edge; the decision log carries what Rails answered.
  test 'a camera that hung up is still compared, because the backends may have stored its frame' do
    out, ok = report(all_seen.reject { |l| l.include?('req0007') })

    refute ok, out
    assert_match(/1 uploads Rails decided that the shadow never saw/, out)
  end

  test 'an upload the shadow never saw fails' do
    out, ok = report(all_seen.reject { |l| l.include?('req0004') })

    refute ok, out
    assert_match(/1 uploads Rails decided that the shadow never saw/, out)
  end
end
