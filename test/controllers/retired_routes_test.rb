# frozen_string_literal: true

require 'test_helper'

# /binaries was retired in 2026-08. Its remaining traffic was scripted -- 25 of
# 26 fetches in the fortnight before carried a curl or Wget agent -- so it
# answers 410 rather than falling through to the catch-all, which redirects to
# the homepage and appends every unmatched URL to a publicly readable file.
class RetiredRoutesTest < ActionDispatch::IntegrationTest
  test 'the retired binaries page says it is gone' do
    get '/binaries'
    assert_response :gone
  end

  test 'it says so for every verb and format a script might use' do
    %i[get post head].each do |verb|
      send(verb, '/binaries')
      assert_response :gone, "#{verb.upcase} /binaries should be 410"
    end
    get '/binaries.json'
    assert_response :gone
  end

  test 'other unknown paths still redirect as they did before' do
    get '/no-such-page-at-all'
    assert_redirected_to '/'
  end

  test 'an unmatched route is logged, not written into the served directory' do
    # public/notfound.txt used to collect every unmatched URL and its referer in
    # the directory the app serves, and answered 200 to anyone who asked for it.
    served = Rails.public_path.join('notfound.txt')
    FileUtils.rm_f(served)

    get '/no-such-page-at-all', headers: { 'HTTP_REFERER' => 'https://example.test/private/page' }

    assert_not File.exist?(served), 'the referer was written into a publicly served file'
  end
end
