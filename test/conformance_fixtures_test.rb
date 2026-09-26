# frozen_string_literal: true

require 'test_helper'
require 'conformance_fixtures'

# The conformance suite replays Rails' verdicts from committed files (#291), so
# a file that no longer says what Rails says would hold a port to a rule this
# application has stopped following. Same shape as the i18n export's test.
class ConformanceFixturesTest < ActiveSupport::TestCase
  test 'the committed verdicts are what Rails answers today' do
    stale = ConformanceFixtures.documents.reject do |name, document|
      File.exist?(ConformanceFixtures.path(name)) &&
        File.read(ConformanceFixtures.path(name)) == ConformanceFixtures.json(document)
    end.keys

    assert_empty stale, "#{stale.join(', ')} out of date: run `bin/rails conformance:fixtures` and commit the result."
  end

  # A corpus that only ever says yes, or only no, is not testing a rule.
  test 'the corpus has both verdicts in it, and HEIF among the accepted' do
    cases = ConformanceFixtures.content_types['cases']

    assert(cases.any? { |c| c['accepted'] })
    assert(cases.any? { |c| !c['accepted'] })
    assert(cases.any? { |c| c['prefix'] == 'heic' && c['accepted'] },
           'no HEIF upload is accepted, which is every HEIF camera refused')
  end
end
