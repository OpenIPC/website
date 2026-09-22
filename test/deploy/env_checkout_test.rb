# frozen_string_literal: true

require 'test_helper'
require 'open3'
require 'tmpdir'
require 'fileutils'

# Two checkouts, one per environment (#159).
#
# openipc-deploy and openipc-static read more than their own logic out of the
# checkout they live in -- docker-compose.yml, the installers' payloads, and
# check-bundle.sh, which judges every bundle before it is installed. With one
# checkout serving both environments those were master's rules for dev too,
# so a change to them could not be tried on dev before it landed. On a site
# whose rule is that nothing lands before it has been tried on dev, that is
# backwards.
#
# These tests are about the handover itself. If it silently stopped happening,
# dev would go back to being judged by master and nothing would say so.
class EnvCheckoutTest < ActiveSupport::TestCase
  DEPLOY = Rails.root.join('deploy')

  # A stand-in for the dev checkout: the same script names, with bodies that
  # announce themselves instead of deploying anything.
  def with_dev_checkout
    Dir.mktmpdir do |tmp|
      dev = File.join(tmp, 'deploy-src-dev', 'deploy')
      FileUtils.mkdir_p(dev)
      %w[deploy.sh static.sh].each do |name|
        File.write(File.join(dev, name), <<~SH)
          #!/usr/bin/env bash
          echo "DEV COPY of #{name} ran with: $*"
        SH
        FileUtils.chmod(0o755, File.join(dev, name))
      end
      yield File.dirname(dev)
    end
  end

  def run_script(script, *args, dev_src:)
    Open3.capture2e(
      { 'DEPLOY_SRC_DEV' => dev_src, 'DEPLOY_SRC_PROD' => DEPLOY.dirname.to_s },
      'bash', DEPLOY.join(script).to_s, *args
    )
  end

  %w[static.sh].each do |script|
    test "#{script} hands a dev install to the dev checkout" do
      with_dev_checkout do |dev_src|
        out, = run_script(script, 'dev', 'abc123', dev_src: dev_src)

        assert_match(/DEV COPY of #{Regexp.escape(script)} ran with: dev abc123/, out,
                     "#{script} did not hand over to the dev checkout:\n#{out}")
      end
    end

    test "#{script} does NOT hand production to the dev checkout" do
      # The whole safety argument rests on this. Production is judged by
      # master's rules whatever is on the dev branch.
      with_dev_checkout do |dev_src|
        out, = run_script(script, 'rollback', 'prod', dev_src: dev_src)

        assert_no_match(/DEV COPY/, out,
                        "#{script} sent production through the dev checkout:\n#{out}")
      end
    end

    test "#{script} works when there is no dev checkout at all" do
      # A machine that has not been set up yet, and every developer's laptop.
      out, = run_script(script, 'rollback', 'dev', dev_src: '/nonexistent')

      assert_no_match(/DEV COPY/, out)
      assert_no_match(/No such file/, out, "#{script} broke without a dev checkout:\n#{out}")
    end
  end

  # --- the command must survive the handover -----------------------------
  #
  # The first version rebuilt the argument list as "<env>" instead of passing
  # the original through. `rollback dev` and `verify dev` both arrived as
  # `dev`, which static.sh reads as an INSTALL of whatever `latest` resolves
  # to -- so a read-only verify would have changed the served bundle. Every
  # command gets its own assertion because the failure was silent: the
  # handover happened, it just handed over something else.
  {
    %w[rollback dev] => 'rollback dev',
    %w[verify dev] => 'verify dev',
    %w[dev abc123] => 'dev abc123',
    %w[dev] => 'dev'
  }.each do |argv, expected|
    test "static.sh hands `#{argv.join(' ')}` over unchanged" do
      with_dev_checkout do |dev_src|
        out, = run_script('static.sh', *argv, dev_src: dev_src)

        assert_match(/DEV COPY of static\.sh ran with: #{Regexp.escape(expected)}$/, out,
                     "the command was rewritten on the way over:\n#{out}")
      end
    end
  end

  test 'deploy.sh hands nothing over, because the compose model is shared' do
    # One docker compose project and one .env carrying both PROD_TAG and
    # DEV_TAG. A handover would have dev writing a different .env from
    # production's, and compose rejecting the tag that is missing from it.
    with_dev_checkout do |dev_src|
      out, = run_script('deploy.sh', 'dev', 'abc123', dev_src: dev_src)

      assert_no_match(/DEV COPY/, out,
                      "deploy.sh handed over; the two environments share one compose model:\n#{out}")
    end
  end

  test 'the dev copy is not asked to hand over again' do
    # Without the guard the dev copy would dispatch straight back into itself.
    with_dev_checkout do |dev_src|
      out, = Open3.capture2e(
        { 'DEPLOY_SRC_DEV' => dev_src, 'OPENIPC_DEPLOY_REEXEC' => '1' },
        'bash', DEPLOY.join('static.sh').to_s, 'rollback', 'dev'
      )

      assert_no_match(/DEV COPY/, out, "the re-exec guard did not hold:\n#{out}")
    end
  end

  test 'each environment is measured against the branch it tracks' do
    # A dev checkout sitting on master is as stale as a production checkout
    # sitting on a feature branch, and the warning has to name the right one.
    out, = Open3.capture2e(
      'bash', '-c',
      ". #{DEPLOY.join('env-checkout.sh')}; checkout_branch_for dev; checkout_branch_for prod"
    )

    assert_equal %w[dev master], out.split
  end
end
