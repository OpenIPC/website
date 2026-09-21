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
end
