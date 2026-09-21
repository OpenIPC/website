# frozen_string_literal: true

require 'open3'
require 'tmpdir'
require 'test_helper'

# /srv/www/deploy-src is not a copy of how openipc.org is deployed. It IS what
# runs: deploy.sh reads docker-compose.yml and legacy-images from beside
# itself, the installers read their payloads from beside themselves, and
# /usr/local/sbin/openipc-deploy and openipc-static are symlinks into it.
#
# On 2026-09-21 it was 32 commits behind master and dirty, and nothing said so
# (#256). The drift was self-sustaining: #239 had added a compose mount the
# donate page needs, the stale checkout would have dropped it on the next
# deploy, so somebody hand-edited the file on the host -- which is what made
# `git pull` refuse, which is what kept the checkout stale.
#
# This is the warning that would have said so, and the guarantee that it can
# only ever warn.
class CheckoutFreshnessTest < ActiveSupport::TestCase
  HELPER = Rails.root.join('deploy/checkout-status.sh')
  SCRIPTS = { 'deploy.sh' => 'do_deploy', 'static.sh' => 'do_install' }.freeze

  def sh(script)
    Rails.root.join('deploy', script).read
  end

  # Directives only: the helper explains the failure mode at length, and the
  # prose names the same functions the assertions look for.
  def directives(text)
    text.lines.reject { |l| l.strip.start_with?('#') }.join
  end

  test 'the helper parses' do
    assert HELPER.exist?, 'deploy/checkout-status.sh is missing'
    _out, status = Open3.capture2e('bash', '-n', HELPER.to_s)
    assert status.success?, 'deploy/checkout-status.sh does not parse'
  end

  SCRIPTS.each do |script, entry|
    test "#{script} warns before it does anything" do
      body = directives(sh(script))[/^#{entry}\(\) \{(.*?)^\}/m, 1]

      assert body, "#{script} has no #{entry}"
      assert_match(/\A\s*checkout_warn /, body,
                   "#{entry} in #{script} does work before saying the checkout it reads from is stale")
    end

    test "#{script} reports the checkout in its status" do
      body = directives(sh(script))[/^do_status\(\) \{(.*?)^\}/m, 1]

      assert body, "#{script} has no do_status"
      assert_includes body, 'checkout_report',
                      "#{script} status says nothing about the checkout it is running out of"
    end

    # An older checkout does not carry the helper -- which is precisely the
    # state being fixed, and it must not also be a crash. The no-ops have to be
    # defined BEFORE the source line, or the real ones would be overwritten.
    test "#{script} still runs from a checkout that has no helper" do
      text = directives(sh(script))
      fallback = text.index('checkout_warn() { :; }')
      source = text.index('checkout-status.sh"')

      assert fallback, "#{script} has no fallback for a checkout without the helper"
      assert source, "#{script} never sources the helper"
      assert fallback < source,
             "#{script} defines its no-op fallbacks after sourcing, which overwrites the real ones"
    end
  end

  # The whole design rests on this. A stale documentation file must never be
  # able to stop a release going out, and an auto-pull would have silently
  # discarded the hand-edit that was keeping production working.
  test 'the warning can only warn' do
    helper = directives(HELPER.read)
    # Single-quoted strings removed first: the helper PRINTS `git -C ... pull
    # --ff-only` as advice, and an assertion that cannot tell that from running
    # it would fail on the very line that makes the warning useful.
    executed = helper.gsub(/'[^']*'/, '')

    assert_no_match(/\bexit\b/, executed, 'checkout-status.sh can exit, so it can abort a deploy')
    assert_no_match(/git\s+(-C\s+\S+\s+)?(pull|merge|reset|checkout)\b/, executed,
                    'checkout-status.sh changes the checkout instead of reporting on it')
    %w[checkout_warn checkout_report].each do |fn|
      body = helper[/^#{fn}\(\) \{(.*?)^\}/m, 1]
      assert body, "checkout-status.sh has no #{fn}"
      assert_match(/return 0\s*\z/, body.strip,
                   "#{fn} can return non-zero, which under `set -e` would kill the caller")
    end
  end

  # The behavioural half, with a local origin so it needs no network.
  test 'it measures a checkout that is behind, and stays quiet about one that is not' do
    Dir.mktmpdir do |tmp|
      git = ->(*args) { Open3.capture2e('git', '-c', 'user.email=t@t', '-c', 'user.name=t', *args) }

      git.call('init', '-q', '--bare', "#{tmp}/origin.git")
      git.call('clone', '-q', "#{tmp}/origin.git", "#{tmp}/work")
      File.write("#{tmp}/work/a", '1')
      git.call('-C', "#{tmp}/work", 'add', '-A')
      git.call('-C', "#{tmp}/work", 'commit', '-qm', 'one')
      git.call('-C', "#{tmp}/work", 'push', '-q', 'origin', 'HEAD:master')

      git.call('clone', '-q', "#{tmp}/origin.git", "#{tmp}/stale")
      assert_empty warn_for("#{tmp}/stale"), 'a current checkout was reported as stale'

      File.write("#{tmp}/work/b", '2')
      git.call('-C', "#{tmp}/work", 'add', '-A')
      git.call('-C', "#{tmp}/work", 'commit', '-qm', 'two')
      git.call('-C', "#{tmp}/work", 'push', '-q', 'origin', 'HEAD:master')

      assert_match(/1 commit\(s\) behind master/, warn_for("#{tmp}/stale"),
                   'a checkout one release behind master was not reported')

      File.write("#{tmp}/stale/a", 'hand-edited on the host')
      assert_match(/has local modifications/, warn_for("#{tmp}/stale"),
                   'a hand-edit on the host was not reported')
    end
  end

  # Counting only what master has and the checkout does not calls a FEATURE
  # BRANCH current, and putting a branch here to try it on dev is a thing this
  # project actually does -- the runbook says to repoint at master after the
  # merge, which is a step somebody has to remember rather than something that
  # is checked. A checkout ahead of master is running deploy code that has not
  # landed, which is the same problem as running code that is out of date.
  test 'a checkout carrying commits master does not have is reported' do
    Dir.mktmpdir do |tmp|
      git = ->(*args) { Open3.capture2e('git', '-c', 'user.email=t@t', '-c', 'user.name=t', *args) }

      git.call('init', '-q', '--bare', "#{tmp}/origin.git")
      git.call('clone', '-q', "#{tmp}/origin.git", "#{tmp}/work")
      File.write("#{tmp}/work/a", '1')
      git.call('-C', "#{tmp}/work", 'add', '-A')
      git.call('-C', "#{tmp}/work", 'commit', '-qm', 'one')
      git.call('-C', "#{tmp}/work", 'push', '-q', 'origin', 'HEAD:master')

      git.call('clone', '-q', "#{tmp}/origin.git", "#{tmp}/branchy")
      assert_empty warn_for("#{tmp}/branchy"), 'a current checkout was reported as drifted'

      # A branch put here to try it on dev, exactly as the runbook describes.
      git.call('-C', "#{tmp}/branchy", 'checkout', '-q', '-b', 'try-something')
      File.write("#{tmp}/branchy/b", '2')
      git.call('-C', "#{tmp}/branchy", 'add', '-A')
      git.call('-C', "#{tmp}/branchy", 'commit', '-qm', 'not landed yet')

      warning = warn_for("#{tmp}/branchy")
      assert_match(/1 commit\(s\) master does not/, warning,
                   'a checkout ahead of master was reported as current')
      assert_match(/try-something/, warning, 'the warning does not name the branch it is on')
    end
  end

  # Behind and ahead at once, which is what a host that was hand-fixed and then
  # left behind looks like. Both facts have to be said: the pull the first
  # warning suggests will refuse, and the second is the reason why.
  test 'a diverged checkout is reported in both directions' do
    Dir.mktmpdir do |tmp|
      git = ->(*args) { Open3.capture2e('git', '-c', 'user.email=t@t', '-c', 'user.name=t', *args) }

      git.call('init', '-q', '--bare', "#{tmp}/origin.git")
      git.call('clone', '-q', "#{tmp}/origin.git", "#{tmp}/work")
      File.write("#{tmp}/work/a", '1')
      git.call('-C', "#{tmp}/work", 'add', '-A')
      git.call('-C', "#{tmp}/work", 'commit', '-qm', 'one')
      git.call('-C', "#{tmp}/work", 'push', '-q', 'origin', 'HEAD:master')
      git.call('clone', '-q', "#{tmp}/origin.git", "#{tmp}/host")

      File.write("#{tmp}/work/b", '2')
      git.call('-C', "#{tmp}/work", 'add', '-A')
      git.call('-C', "#{tmp}/work", 'commit', '-qm', 'master moved on')
      git.call('-C', "#{tmp}/work", 'push', '-q', 'origin', 'HEAD:master')

      File.write("#{tmp}/host/c", '3')
      git.call('-C', "#{tmp}/host", 'add', '-A')
      git.call('-C', "#{tmp}/host", 'commit', '-qm', 'hand-fixed on the host')

      warning = warn_for("#{tmp}/host")
      assert_match(/1 commit\(s\) behind master/, warning, 'the diverged checkout was not reported as behind')
      assert_match(/1 commit\(s\) master does not/, warning, 'the diverged checkout was not reported as ahead')
    end
  end

  # An rsynced copy is not a checkout and has nothing to be stale against.
  # Saying so on every run would train people to ignore the warning.
  test 'it says nothing about a directory that is not a checkout' do
    Dir.mktmpdir { |tmp| assert_empty warn_for(tmp) }
  end

  private

  def warn_for(dir)
    out, = Open3.capture2e('bash', '-c', ". '#{HELPER}'; checkout_warn '#{dir}'")
    out.gsub(/\e\[[0-9;]*m/, '').strip
  end
end
