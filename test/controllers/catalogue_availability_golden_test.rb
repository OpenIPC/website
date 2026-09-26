# frozen_string_literal: true

require 'test_helper'

# The availability feed, byte for byte, from the real catalogue (#289).
#
# The feed tells 378 prerendered pages what a visitor can do with each chip,
# and it was read from the socs table until the table stopped being the
# catalogue. fixtures/files/catalogue/availability.json is what the table-backed
# code answered, captured on master with the table seeded from data/catalogue
# and the production release index of 2026-09-24 beside it; that index is the
# release-index.json next to it. Switching the source must not move a byte.
#
# When data/catalogue changes, this changes with it. Regenerate the expected
# file with GOLDEN=1 and read the diff: it should be exactly the chips that
# were edited.
class CatalogueAvailabilityGoldenTest < ActionDispatch::IntegrationTest
  FIXTURES = Rails.root.join('test/fixtures/files/catalogue')

  setup do
    Catalogue.current = Catalogue.load
    @root = Dir.mktmpdir
    FileUtils.cp(FIXTURES.join('release-index.json'), File.join(@root, '.index.json'))
    ENV['RELEASE_INDEX_ROOT'] = @root
    ReleaseIndex.reset!
  end

  teardown do
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(@root)
  end

  test 'the feed says exactly what the table-backed feed said' do
    get '/api/v1/hardware/availability.json'
    assert_response :success

    answered = response.body.sub(/"generated_at":"[^"]*"/, '"generated_at":"X"')
    expected = FIXTURES.join('availability.json')
    File.write(expected, answered) if ENV['GOLDEN']

    assert_equal expected.read, answered, <<~MESSAGE.chomp
      The availability feed no longer matches #{expected.relative_path_from(Rails.root)}.
      If data/catalogue changed, regenerate it with GOLDEN=1 and check the diff
      names only the chips that were edited. Otherwise the feed has changed what
      it tells the hardware pages.
    MESSAGE
  end
end
