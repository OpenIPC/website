# frozen_string_literal: true

require 'test_helper'

# ?locale=xx answers 301 to the prefixed path (#154).
#
# This is the half of the locale change that cannot be taken back: it moves
# addresses other people have linked to. The prefixed URLs, the hreflang set
# and sitemap.xml shipped first and separately, so that a ranking movement
# afterwards can be attributed to one or the other.
class QueryLocaleRedirectTest < ActionDispatch::IntegrationTest
  test 'a query locale becomes a path prefix' do
    get '/donate?locale=ru'

    assert_response :moved_permanently
    assert_equal '/ru/donate', URI(response.location).path
  end

  test 'the root becomes the bare prefix rather than a trailing slash' do
    get '/?locale=zh'

    assert_equal '/zh', URI(response.location).path
  end

  # 301, not 302: these are indexed URLs and the point is to transfer them.
  test 'it is permanent' do
    get '/get-started?locale=zh'

    assert_equal 301, response.status
  end

  # English is served at the bare path, so the parameter is simply dropped.
  test 'the default locale loses the parameter and keeps the path' do
    get '/donate?locale=en'

    assert_response :moved_permanently
    assert_equal '/donate', response.location.sub('http://www.example.com', '')
  end

  # What the old switcher merge existed to protect: switching language on a
  # filtered list must not lose the filter.
  test 'the rest of the query string survives' do
    get '/supported-hardware/featured?vendor=hisilicon&locale=zh'

    assert_equal '/zh/supported-hardware/featured', URI(response.location).path
    assert_equal 'vendor=hisilicon', URI(response.location).query
  end

  test 'an already prefixed path does not redirect to itself' do
    get '/ru/donate?locale=zh'

    assert_response :success
    assert_select 'html[lang=?]', 'ru'
  end

  test 'the redirect lands somewhere that renders in the right language' do
    get '/donate?locale=ru'
    follow_redirect!

    assert_response :success
    assert_select 'html[lang=?]', 'ru'
  end

  # The snapshot views and the catalogue were the examples here until #154
  # localized them. What is left is the secondary Open Wall entry points --
  # /open-wall/<page> and /open-wall/camera/<id> -- which have no prefixed
  # form. Redirecting one of those would send a Russian reader to the English
  # homepage through the catch-all, so they keep the old mechanism, and this
  # is the path that proves the branch still exists.
  test 'a page with no prefixed form is left alone' do
    get '/open-wall/2?locale=ru'

    assert_response :success
    assert_select 'html[lang=?]', 'ru'
  end

  # The other half of #154: these two used to be the example above.
  test 'the pages that gained a prefixed form now use it' do
    get '/snapshots?locale=ru'
    assert_redirected_to '/ru/snapshots'

    get '/supported-hardware/featured?locale=zh'
    assert_redirected_to '/zh/supported-hardware/featured'
  end

  test 'the admin area is not redirected' do
    get '/admin/sign_in?locale=ru'

    assert_response :success
  end

  # The one request on this site that can never be touched: firmware in the
  # field cannot be upgraded, so POST /snapshots must stay exactly where the
  # cameras post it, body intact.
  test 'a POST is never redirected' do
    post '/snapshots?locale=ru', params: { mac_address: '00:11:22:33:44:55' }

    assert_not_equal 301, response.status
  end

  # English is the bare path, so ?locale=en normally just loses the parameter.
  # Not on a page with no prefixed form: there the parameter is the only thing
  # saying English, and dropping it hands the visitor back to the session and
  # the browser header. A Russian browser asking for English got Russian.
  test 'a page with no prefixed form keeps its English parameter' do
    russian = { 'HTTP_ACCEPT_LANGUAGE' => 'ru' }

    get '/open-wall/2', headers: russian
    assert_select 'html[lang=?]', 'ru'

    get '/open-wall/2?locale=en', headers: russian

    assert_response :success
    assert_select 'html[lang=?]', 'en'
  end

  test 'a locale this site does not serve is ignored' do
    get '/donate?locale=xx'

    assert_response :success
  end
end
