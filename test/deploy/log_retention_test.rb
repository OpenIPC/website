# frozen_string_literal: true

require 'test_helper'

# The fourteen days /privacy promises, tied to the file that delivers them
# (#227).
#
# The page says the server log is deleted after fourteen days. That number was
# `rotate 14` in Debian's stock /etc/logrotate.d/nginx -- a file this
# repository had never seen, on a host that comes back from
# deploy/RESTORE.md with whatever the distribution ships. Nothing here could
# fail when it changed, and test/controllers/privacy_claims_test.rb said so in
# a comment rather than a test.
#
# Two things measured on the origin on 2026-09-21, which is why this file
# checks more than the number:
#
#   /var/log/nginx           45 files, 12.3 MB older than fourteen days
#   /srv/www/org-openipc/log 152 files, 90 MB, back to 2022-07-14
#
# Both hold visitor addresses. The first happened because `rotate 14` counts
# rotations and `notifempty` stops them: openipc.net went quiet on 20 August
# and its fourteen rotated logs froze there. The second is the retired
# bare-metal deployment's Rails log, under `rotate 365`, frozen the same way on
# the day the container took over.
class LogRetentionTest < ActiveSupport::TestCase
  POLICY_DIR = Rails.root.join('deploy/logrotate.d')
  LOCALES = %w[en ru zh].freeze

  # Directives only. Both files are mostly commentary, and the commentary
  # necessarily names `notifempty` and `rotate 14` while explaining them, so a
  # test that greps the raw text reports the opposite of the truth for exactly
  # the assertions that matter most here.
  def directives(name)
    POLICY_DIR.join(name).read.lines
              .reject { |line| line.strip.start_with?('#') || line.strip.empty? }
              .map(&:strip)
  end

  def retention(name)
    values = directives(name).filter_map { |d| Regexp.last_match(2).to_i if d =~ /\A(rotate|maxage)\s+(\d+)\z/ }

    assert_equal 2, values.size, "#{name} must set both `rotate` and `maxage`; it sets #{values.size} of them"
    assert_equal 1, values.uniq.size, "#{name} sets rotate and maxage to #{values.inspect}, so neither is the retention"

    values.first
  end

  def claim(locale)
    I18n.with_locale(locale) { I18n.t('pages.privacy.log_text_html') }
  end

  # --- the number on the page is the number in the file ---

  test 'the retention the privacy page states is the one the policy enforces' do
    days = retention('nginx')

    LOCALES.each do |locale|
      assert_includes claim(locale).scan(/\d+/), days.to_s, <<~MESSAGE.chomp
        deploy/logrotate.d/nginx keeps the server log for #{days} days, and the
        #{locale} privacy page does not say #{days}. One of them is a promise to
        visitors about their addresses and the other is what the host does;
        they have to be the same number.

          #{claim(locale)}
      MESSAGE
    end
  end

  # One promise covers every directory on this host that holds addresses, so
  # the retired deployment's Rails log cannot quietly keep a different one --
  # which for four years it did, at `rotate 365`.
  test 'both directories that hold visitor addresses keep them for the same time' do
    assert_equal retention('nginx'), retention('openipc'), <<~MESSAGE.chomp
      The nginx logs and /srv/www/org-openipc/log both hold visitor addresses
      and /privacy makes one promise about them. Whichever of these is longer
      is what the host actually does, and the page would be stating the other.
    MESSAGE
  end

  # --- days, not rotations ---

  # `rotate 14` alone is fourteen days only while the log is written every day.
  # maxage is what makes it a duration, and logrotate(8) is explicit that "the
  # age is only checked if the logfile is to be rotated" -- so notifempty,
  # which skips the rotation of an empty log, also skips the ageing.
  #
  # Confirmed against a copy of the real file set before this was written:
  # stock left 27 over-age files, notifempty + maxage left 14, and maxage
  # without notifempty left none.
  test 'a log that stops being written still ages out' do
    POLICY_DIR.children.map { |f| f.basename.to_s }.each do |name|
      assert_not_includes directives(name), 'notifempty', <<~MESSAGE.chomp
        deploy/logrotate.d/#{name} sets notifempty, which stops rotating a log
        once it is empty -- and maxage is only checked when a log is rotated.
        A vhost that goes quiet therefore keeps its last fourteen rotated logs,
        with the addresses in them, for as long as the host exists. That is how
        openipc.net's logs from 6 August were still here on 21 September.
      MESSAGE

      assert_includes directives(name).grep(/\Amaxage /), "maxage #{retention(name)}", <<~MESSAGE.chomp
        deploy/logrotate.d/#{name} has no maxage, so its retention is a count of
        rotations rather than a number of days. With `daily` those agree only
        while the log is written every day.
      MESSAGE
    end
  end

  # --- nothing lands outside the policy ---

  # The alternative the issue proposed was naming each vhost's logs explicitly
  # so a new one could not be missed. It would do the opposite: a glob covers a
  # new vhost the day it appears, while a list leaves it outside rotation until
  # somebody remembers this file. This asserts the property the list was meant
  # to buy, without giving up the glob.
  test 'every log the committed nginx configuration writes is covered' do
    patterns = directives('nginx').grep(/\{\z/).map { |line| line.sub(/\s*\{\z/, '') }
                                  .flat_map(&:split)

    refute_empty patterns, 'deploy/logrotate.d/nginx matches no files at all'

    paths = Rails.root.glob('deploy/nginx/**/*').select(&:file?).flat_map do |file|
      # Up to the `;` or the next argument: `access_log /path/x.log openipc;`
      # and `error_log /path/x.log info;` both carry one, and a captured
      # trailing semicolon matches no glob.
      file.read.scan(%r{^\s*(?:access_log|error_log)\s+(/[^\s;]+)}).flatten
    end.uniq

    refute_empty paths, 'no access_log or error_log was found in deploy/nginx; this test is reading nothing'

    uncovered = paths.reject { |path| patterns.any? { |glob| File.fnmatch(glob, path) } }

    assert_empty uncovered, <<~MESSAGE.chomp
      These logs are written by the committed nginx configuration and are not
      matched by deploy/logrotate.d/nginx, so they would be kept forever:

        #{uncovered.join("\n  ")}

      Covered by: #{patterns.join(', ')}
    MESSAGE
  end

  # --- the host gets it ---

  INSTALLER = Rails.root.join('deploy/install-logrotate.sh')

  # The script's own commentary explains the traps below, which means it names
  # `/etc/logrotate.d`, `.bak-` and the rest while doing so. Same rule as for
  # the policy files: read what runs, not what explains it.
  def script
    INSTALLER.read.lines.reject { |line| line.strip.start_with?('#') }.map(&:strip)
  end

  # logrotate 3.22 prints `error: <file>:4 argument expected after maxage
  # count` and exits 0, which was checked on the origin rather than assumed.
  # An installer that tests only the exit status therefore installs a broken
  # policy and prints its success line over it -- and a config file logrotate
  # cannot parse stops every log on the host from rotating, not just these two.
  test 'the installer does not trust logrotate exit status alone' do
    assert_includes script.join("\n"), "grep -q '^error'", <<~MESSAGE.chomp
      deploy/install-logrotate.sh has to read logrotate's output for error
      lines, not just its exit code: logrotate reports a syntax error on
      stderr and exits 0 anyway.
    MESSAGE
  end

  # Validate the staged copy, then install. The other order leaves a policy
  # logrotate cannot parse sitting under /etc/logrotate.d whenever the rollback
  # has nothing to restore -- a destination that did not exist before, or an
  # install that fails on the second file -- and one unreadable file there ends
  # the whole run, so every log on the host stops rotating and not just these.
  test 'nothing reaches /etc/logrotate.d before the policy has been parsed' do
    lines = script
    validates = lines.index { |l| l.start_with?('out=$(logrotate --debug') }
    installs = lines.index { |l| l.include?('install -m 0644') && l.include?('"$dest/') }

    assert validates, 'deploy/install-logrotate.sh never runs logrotate over what it is about to install'
    assert installs, 'deploy/install-logrotate.sh never installs anything into $dest'

    assert validates < installs, <<~MESSAGE.chomp
      deploy/install-logrotate.sh writes to $dest at line #{installs + 1} and
      only parses the policy at line #{validates + 1}. A broken file then has to
      be rolled back, and rollback cannot help a destination that had no
      previous version -- which is every file on a freshly rebuilt host, the
      one case RESTORE.md exists for.
    MESSAGE
  end

  # logrotate's `include` reads every file in the directory except the taboo
  # extensions -- .bak, .old, .orig, .dpkg-*, and the rest of a fixed list.
  # `nginx.bak-20260921T000000Z` is not one of them, because its suffix is the
  # timestamp, so a backup left beside the policy is read as a second policy
  # for the same paths. Checked on the origin: logrotate answers
  # `error: duplicate log entry for ...` and exits 1.
  test 'backups are not left where logrotate will read them as policy' do
    target = script.grep(/\Abackups=/).first

    assert target, 'deploy/install-logrotate.sh does not say where it keeps the previous policy'

    path = target.split('=', 2).last

    assert_not_includes path, '/etc/logrotate.d', <<~MESSAGE.chomp
      deploy/install-logrotate.sh keeps its backups in #{path}. logrotate reads
      every non-taboo file in its include directory, and a timestamped backup is
      not taboo -- so the next run finds two policies for the same logs and
      fails on `duplicate log entry`.
    MESSAGE
  end

  test 'the policy has an installer and RESTORE.md names it' do
    installer = Rails.root.join('deploy/install-logrotate.sh')

    assert_predicate installer, :exist?, 'deploy/logrotate.d/ is policy nothing puts on the host'
    assert installer.stat.mode & 0o111 != 0, 'deploy/install-logrotate.sh is not executable'

    assert_includes Rails.root.join('deploy/RESTORE.md').read, 'install-logrotate.sh', <<~MESSAGE.chomp
      A rebuilt host follows RESTORE.md. If the log-rotation step is not in it,
      the host comes back keeping visitor addresses for the distribution's
      default rather than the fourteen days the site promises -- which is the
      whole of #227.
    MESSAGE
  end
end
