# frozen_string_literal: true

require 'test_helper'
require 'shellwords'
require 'English'

# deploy/publish-release-index.rb asked GitHub for one page of releases and
# indexed whatever came back. openipc/firmware had 102 releases on 2026-09-22
# and the page size was 30, so seventy-two of them were invisible to the
# index -- and since the release mirror was retired, an asset missing from the
# index is a download the site refuses outright while the file sits on GitHub,
# reachable, the whole time.
#
# The script talks to the network and writes to /srv, so it is not run here.
# These read it. That is worth doing anyway: the failure was silent, it lasted
# months, and nothing in the repository would have said a word.
class ReleaseIndexPagingTest < ActiveSupport::TestCase
  # .freeze because frozen_string_literal does not cover a String that came
  # back from IO. A shared constant every test reads is exactly the thing that
  # should not be mutable.
  SCRIPT = Rails.root.join('deploy/publish-release-index.rb').read.freeze

  test 'the page size is the API maximum' do
    # 100 is as many as GitHub will return at once. Anything smaller is more
    # requests for the same releases; anything larger is silently clamped to
    # 100, which would make the short-page test below stop early and lose the
    # rest of the list.
    assert_match(/^RELEASES_PER_PAGE = 100$/, SCRIPT)
  end

  test 'it asks for more than one page' do
    assert_match(/def all_releases/, SCRIPT, 'nothing pages through the release list')
    assert_match(/releases_page\(page\)/, SCRIPT, 'the fetch does not take a page number')
    assert_match(/page: page/, SCRIPT, 'the page number never reaches the API call')
  end

  test 'it stops on a short page, not on a fixed count' do
    # A page shorter than the page size is the end of the list. Stopping on a
    # count is how the original got it wrong.
    assert_match(/return releases if batch\.size < RELEASES_PER_PAGE/, SCRIPT)
  end

  test 'the backstop says so rather than truncating quietly' do
    assert_match(/MAX_RELEASE_PAGES/, SCRIPT)
    assert_match(/release list is still going after/, SCRIPT,
                 'hitting the page cap must be logged; indexing a prefix in silence ' \
                 'is the bug this change exists to fix')
  end

  test 'the caller takes every release, not one page' do
    assert_match(/releases = all_releases/, SCRIPT)
    assert_no_match(/releases = releases_page$/, SCRIPT)
  end

  test 'the script still parses' do
    # These tests are textual, so a change that satisfies every regex and
    # breaks the file would pass them all.
    out = `ruby -c #{Shellwords.escape(Rails.root.join('deploy/publish-release-index.rb').to_s)} 2>&1`
    skip 'ruby is not on PATH in this environment' unless $CHILD_STATUS&.success? || out.present?

    assert_match(/Syntax OK/, out, out)
  end
end
