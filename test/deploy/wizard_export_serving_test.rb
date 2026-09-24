# frozen_string_literal: true

require 'test_helper'

# How the wizard's command blocks reach the page (#164).
#
# They cannot be built with the bundle: what a bootloader is told to do depends
# on what upstream has published, and the release index that says so is a file
# on the host while the bundle is built in CI. So they are written hourly and
# fetched, as the backer count is -- and that arrangement has four parts which
# have to agree with each other.
class WizardExportServingTest < ActiveSupport::TestCase
  VHOSTS = %w[org.openipc org.openipc.dev].freeze

  def vhost(name)
    Rails.root.join('deploy/nginx/sites-available', name).read
  end

  test 'both vhosts serve the export, each from its own directory' do
    # Not one directory between them. The file is written by running the
    # container, so dev writes what dev's code renders -- and dev is where a
    # change to that shape is tried. Sharing would have a dev deploy quietly
    # rewrite what production's pages read, which is the one thing that
    # environment exists to make impossible.
    { 'org.openipc' => '/srv/www/shared/wizard',
      'org.openipc.dev' => '/srv/www/shared/wizard-dev' }.each do |name, directory|
      config = vhost(name)

      assert_match %r{location ~ \^/api/v1/wizard/}, config, "#{name} does not serve the export"
      assert_match %r{alias #{Regexp.escape(directory)}/}, config,
                   "#{name} serves the export from somewhere other than #{directory}"
    end
  end

  test 'the address it accepts is the slug the models allow, and nothing else' do
    # The name in the URL becomes a filename. Vendor and Soc refuse a urlname
    # that is not a safe slug (#275), and this is the other end of the same
    # rule: a request that is not one does not match the location at all.
    VHOSTS.each do |name|
      pattern = vhost(name)[%r{location ~ \^/api/v1/wizard/\(\?<soc_slug>([^)]+)\)}, 1]

      assert pattern, "#{name} has no capture for the SoC"
      assert_equal '[a-z0-9][a-z0-9._-]*', pattern,
                   "#{name} accepts a different shape from Soc::URLNAME_FORMAT"
    end

    %w[../../etc/passwd foo/bar .hidden ''].each do |bad|
      assert_no_match(/\A[a-z0-9][a-z0-9._-]*\z/, bad)
    end
  end

  test 'a missing file answers JSON rather than the catch-all' do
    # 49 SoCs have nothing published and so have no file. The page treats that
    # as "no commands to show" and says so; an HTML error page would arrive at
    # a `fetch` expecting JSON.
    VHOSTS.each do |name|
      config = vhost(name)

      assert_match(/error_page 404 = @no_wizard_export/, config, name)
      assert_match(/location @no_wizard_export \{[^}]*return 404/m, config, name)
    end
  end

  test 'something writes the files, on a schedule, and it is installed' do
    cron = Rails.root.join('deploy/cron.d/openipc-metrics').read
    installer = Rails.root.join('deploy/install-metrics.sh').read
    runner = Rails.root.join('deploy/wizard-export.sh')

    assert_path_exists runner
    assert_match(/openipc-wizard-export/, cron, 'nothing refreshes the export')

    # Both environments, on their own schedules. The wrapper defaults to
    # production's container and production's directory, so a single entry
    # refreshed production and left dev on whatever somebody last ran by hand
    # -- and a fresh dev host with no export at all shows "the commands could
    # not be loaded" on every wizard page. Found by review on #276.
    dev = cron.lines.find { |line| line.include?('openipc-web-dev') }
    assert dev, 'nothing refreshes the export dev serves'
    assert_includes dev, 'WIZARD_EXPORT_DIR=/srv/www/shared/wizard-dev',
                    "dev's scheduled export does not write where the dev vhost reads"
    assert_match(%r{>>/var/log/openipc-wizard-dev\.log}, dev,
                 "dev's export shares production's log, so a failure in one reads as the other")

    prod = cron.lines.find { |line| line.include?('openipc-wizard-export') && !line.include?('-dev') }
    assert prod, 'nothing refreshes the export production serves'
    assert_not_includes prod, 'WIZARD_EXPORT_CONTAINER',
                        "production's export should take the wrapper's defaults"

    assert_match(%r{install .*wizard-export\.sh}, installer, 'the runner is never installed')
    assert_match(%r{install -d .*\$wizarddir}, installer, 'nothing creates the directory nginx reads')
    assert_match(%r{install -d .*\$wizarddevdir}, installer, "nothing creates dev's own directory")

    # Owned by the application, which is what writes it. Created root-owned, it
    # was a directory the job could open and not write into.
    assert_match(/^appuid=1000$/, installer, 'the installer does not name the uid the image runs as')
    %w[wizarddir wizarddevdir].each do |name|
      assert_match(/install -d -m 0755 -o "\$appuid" -g "\$appuid" "\$#{name}"/, installer,
                   "$#{name} is not created owned by the application")
    end
  end

  test 'the job writes where nginx reads' do
    runner = Rails.root.join('deploy/wizard-export.sh').read
    served = vhost('org.openipc')[%r{alias (/srv/www/shared/wizard)/}, 1]

    assert_match(/WIZARD_EXPORT_DIR:-#{Regexp.escape(served)}/, runner,
                 'the job and nginx disagree about where the files live')
  end

  test 'each container can write its own export and cannot see the other' do
    # The application's view of /srv/www/shared is read-only, deliberately, so
    # the one directory under it that it does write is mounted on its own. The
    # first version of the job passed the HOST path into the container, where
    # nothing is mounted at it: `mkdir_p` then failed at /srv, naming a
    # directory that had nothing to do with the export.
    compose = Rails.root.join('deploy/docker-compose.yml').read
    runner = Rails.root.join('deploy/wizard-export.sh').read

    inside = runner[%r{^INSIDE=(\S+)}, 1]
    assert inside, 'the job does not say where the directory is inside the container'
    assert_match(/WIZARD_EXPORT_DIR=\$INSIDE/, runner, 'the job passes the host path into the container')

    %w[/srv/www/shared/wizard /srv/www/shared/wizard-dev].each do |host_path|
      assert_includes compose, "- #{host_path}:#{inside}",
                      "#{host_path} is not mounted where the job writes"
    end

    # And read-only where it must stay so: the backer count is printed by the
    # site and owned by a job on the host.
    assert_match(%r{- /srv/www/shared:/rails/shared:ro}, compose,
                 'the shared directory is no longer read-only to the application')
  end
end
