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

  test 'an unmatched route writes nothing into the served directory' do
    # public/notfound.txt used to collect every unmatched URL and its referer in
    # the directory the app serves, and answered 200 to anyone who asked for it.
    # nginx's own log keeps that record, so nothing here replaces it.
    served = Rails.public_path.join('notfound.txt')
    FileUtils.rm_f(served)

    get '/no-such-page-at-all', headers: { 'HTTP_REFERER' => 'https://example.test/private/page' }

    assert_not File.exist?(served), 'the referer was written into a publicly served file'
  end

  # --- the retired wiki host ---
  #
  # wiki.openipc.org proxied to openipc.cloud, which was repurposed into a
  # commercial product site -- so every one of these sent visitors to a shop
  # under our own domain and our own certificate. The wiki source itself never
  # went anywhere: it is github.com/OpenIPC/wiki, which the site navigation has
  # pointed at for some time.

  test 'the wiki redirects go to the wiki source, not the retired host' do
    { '/ru/installation.md' => 'https://github.com/OpenIPC/wiki/blob/master/ru/installation.md',
      '/devices/hs303/' => 'https://github.com/OpenIPC/wiki/blob/master/ru/hardware-hs303.md',
      '/install_switcam_hs303' => 'https://github.com/OpenIPC/wiki/blob/master/ru/hardware-hs303.md' }
      .each do |path, target|
      get path
      assert_redirected_to target
    end
  end

  test 'nothing anywhere in the app points at the retired wiki host' do
    # Nine locale files carried it as well as the three routes, so a test that
    # only walked the routes would have passed while the invitation to "fill up
    # the Wiki" still led to the shop.
    offenders = Dir.glob(Rails.root.join('{app,config}/**/*'))
                   .select { |f| File.file?(f) }
                   .select { |f| File.read(f).include?('wiki.openipc.org') }

    assert_empty offenders, "still pointing at the retired wiki host: #{offenders.join(', ')}"
  end
end
