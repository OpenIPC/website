# frozen_string_literal: true

require 'test_helper'

# The host installers are run by copying deploy/ to the host and executing a
# script out of the copy. `scp -P 35242 -r deploy host:/tmp/openipc-deploy` was
# the documented way to do that, and it is correct exactly once: on a re-run
# the destination already exists, so scp copies the tree INSIDE it as
# /tmp/openipc-deploy/deploy/, and the installer invoked at the old path is the
# one the previous session left there.
#
# It fails by succeeding. On 2026-09-21 that installed a stale
# openipc-audience-report while printing its usual "installed ..." line; the
# only evidence was the checksum not changing.
#
# Nothing else in the suite reads these files, and a docstring has no other
# test, so this is what keeps the unsafe form from coming back by copy-paste.
class InstallDocsTest < ActiveSupport::TestCase
  DOCS = Rails.root.glob('deploy/**/*.{sh,md}').freeze

  # A recursive copy of the deploy tree ONTO a remote path, rather than of its
  # contents INTO one. rsync with a trailing slash on both sides is the form
  # that stays correct on a re-run, and `rm -rf` first would too.
  #
  # The remote target is part of the pattern, so that prose explaining why the
  # form is wrong -- which necessarily names it -- is not itself a match.
  # `deploy/.` does not match either: the whitespace has to follow `deploy`.
  NESTING = /\bscp\b[^\n]*-r\s+deploy\s+\S+:/

  test 'the deploy tree is not copied to the host in a way that nests on a re-run' do
    offenders = DOCS.select { |f| f.read.match?(NESTING) }

    assert_empty offenders.map { |f| f.relative_path_from(Rails.root).to_s }, <<~MESSAGE.chomp
      `scp -r deploy <host>:<dir>` is correct only while <dir> does not exist.
      The second time it copies the tree into <dir>/deploy/, and the installer
      run from <dir> is then the stale one from the previous session --
      which prints success over files it never copied.

      Use the form the installers document:

        rsync -a --delete -e 'ssh -p 35242' deploy/ root@openipc.org:/tmp/openipc-deploy/
    MESSAGE
  end

  # The checksum print is the backstop for the same failure, and it has to
  # survive: without it a stale install is invisible however the copy was made
  # -- including by someone doing it by hand.
  test 'install-metrics says what it installed, not what it meant to install' do
    body = Rails.root.join('deploy/install-metrics.sh').read

    assert_match(/sha256sum "\$f"/, body, <<~MESSAGE.chomp)
      install-metrics.sh has to print a checksum of each file it installs.
      A copy that landed in the wrong place leaves the script reporting
      success over stale files, and the checksum is the only way to see it.
    MESSAGE
  end

  # A checksum you cannot compare against anything is decoration. The script
  # tells the reader which files to hash in their checkout, and the first
  # version of that line said `deploy/*.sh` -- which misses
  # `deploy/openipc-sample-rss`, having no extension, and the cron file, which
  # is a directory down. Two of the four had nothing to compare with.
  test 'the files install-metrics installs are the files it tells you to hash' do
    body = Rails.root.join('deploy/install-metrics.sh').read

    installed = body.scan(%r{^install -m \S+ -o \S+ -g \S+ "\$here/([^"]+)"}).flatten
    documented = body[/sha256sum (.*?)\| cut/m].to_s

    # Counted rather than hardcoded. The number was 4 and is 5 since #198;
    # pinning it turns "the installer grew a file" into a failure that reads
    # like the docs check broke, which is not what this test is about.
    refute_empty installed, 'install-metrics.sh installs nothing; this test proves nothing'
    missing = installed.reject { |f| documented.include?("deploy/#{f}") }

    assert_empty missing, <<~MESSAGE.chomp
      install-metrics.sh installs these and does not tell you how to hash them:

      #{missing.map { |f| "  deploy/#{f}" }.join("\n")}

      The comparison command in the header has to name every file the
      installer installs, or the checksums it prints cover artifacts the
      reader has no source-side output for -- which is how the first version
      of it left two of four unverifiable.
    MESSAGE
  end

  # The copy step is the one piece of the restore that runs before anything
  # else works, and rsync is not on every Debian install. A rebuilt host that
  # reaches the installers without it fails at the transfer, which is a
  # confusing place to discover a missing package.
  test 'RESTORE.md declares the tool its own commands need' do
    restore = Rails.root.join('deploy/RESTORE.md').read
    prerequisites = restore[/### \d+\. Host prerequisites.*?(?=\n## )/m].to_s

    assert_includes restore, 'rsync -a --delete', 'this test is about the documented copy command'
    assert_match(/\brsync\b/, prerequisites, <<~MESSAGE.chomp)
      RESTORE.md tells a rebuilt host to copy deploy/ with rsync, so rsync
      belongs in its prerequisites. It is needed at both ends and a Debian
      install does not always have it.
    MESSAGE
  end
end
