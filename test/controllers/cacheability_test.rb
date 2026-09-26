# frozen_string_literal: true

require 'test_helper'

# The property the whole migration rests on: for a public page, the URL
# determines the response. Until #155 it did not -- every response carried
# Set-Cookie because the locale was kept in the session and a CSRF token was
# minted for pages with no form, and a response carrying Set-Cookie is one most
# caches decline to store. That is why `location /` in the vhost has no cache
# at all and 51% of requests reached Rails for pages that had not changed.
#
# #154 made the address carry the language. This file is what stops the cookie
# coming back.
class CacheabilityTest < ActionDispatch::IntegrationTest
  # Forgery protection is off in the test environment, which makes
  # csrf_meta_tags render nothing -- so every assertion here about cookies and
  # tokens passed whether or not the layout emitted one. Found by reverting
  # the change and watching the test still pass, which is the only way that
  # kind of hole shows up.
  #
  # Turned on for this file alone: it is the one file whose subject is what
  # the response headers look like in production.
  setup do
    @forgery_protection = ActionController::Base.allow_forgery_protection
    ActionController::Base.allow_forgery_protection = true
  end

  teardown { ActionController::Base.allow_forgery_protection = @forgery_protection }

  PUBLIC = ['/', '/ru', '/zh', '/donate', '/ru/donate',
            '/supported-hardware/featured', '/open-wall'].freeze

  test 'no public page sets a cookie' do
    offenders = PUBLIC.filter_map do |path|
      get path
      cookie = response.headers['Set-Cookie']
      "#{path} -> #{cookie}" if cookie.present?
    end

    assert_empty offenders, <<~MESSAGE.chomp
      These public pages still set a cookie:

      #{offenders.map { |o| "  #{o}" }.join("\n")}

      A response carrying Set-Cookie cannot be stored by a shared cache. Find
      what wrote to the session -- the usual causes are a session[] assignment
      and csrf_meta_tags being rendered on a page with no form.
    MESSAGE
  end

  test 'no public page varies by cookie' do
    PUBLIC.each do |path|
      get path

      assert_not_includes response.headers['Vary'].to_s, 'Cookie',
                          "#{path} varies by cookie, so it cannot be shared between visitors"
    end
  end

  test 'public pages say how long they are good for' do
    PUBLIC.each do |path|
      get path
      cache_control = response.headers['Cache-Control'].to_s

      assert_includes cache_control, 'public', "#{path} is not declared public"
      assert_match(/max-age=[1-9]/, cache_control, "#{path} has no positive max-age")
      assert_match(/stale-while-revalidate=[1-9]/, cache_control,
                   "#{path} cannot be served stale while it refreshes, which is what a flood needs")
    end
  end

  # Not just the public pages: nothing the origin answers sets a cookie (#288).
  # The admin's session was the last cookie the site wrote, and with it gone
  # the vhost no longer bypasses its cache for anyone carrying one -- so a
  # cookie reappearing anywhere would be both a privacy regression and a page
  # nginx quietly stops caching. The list covers the ways a response gets
  # written: a page, a redirect that used to carry a flash, a 404, a 410, the
  # catch-all, JSON, and the one POST the public makes.
  ANSWERS = ['/', '/sitemap.xml', '/cameras/socs.json', '/up',
             '/snapshots/ffffffffffffffffffff', '/open-wall/camera/0123456789abcdef',
             '/admin', '/admin/sign_in', '/merchandise', '/no-such-page-anywhere'].freeze

  test 'nothing the origin answers sets a cookie' do
    offenders = ANSWERS.filter_map do |path|
      get path
      cookie_set_by("GET #{path}")
    end
    post '/snapshots', params: {}
    offenders << cookie_set_by('POST /snapshots')
    offenders.compact!

    assert_empty offenders, <<~MESSAGE.chomp
      These responses set a cookie, and since #288 nothing on the site should:

      #{offenders.map { |o| "  #{o}" }.join("\n")}

      The usual causes are a flash on a redirect (`redirect_to ..., alert:`),
      a session[] assignment, or csrf_meta_tags on a page.
    MESSAGE
  end

  def cookie_set_by(request)
    cookie = response.headers['Set-Cookie']
    "#{request} (#{response.status}) -> #{cookie}" if cookie
  end

  test 'the admin is gone, not merely unlinked' do
    get '/admin'
    assert_response :gone
    get '/admin/sign_in'
    assert_response :gone

    assert_not Gem.loaded_specs.key?('devise'), 'Devise is loaded again'
    assert_not ActiveRecord::Base.connection.table_exists?(:admins), 'the admins table is back'
  end

  # The wizard is the one public page with a form, so it is the one that still
  # needs a token -- and the token is what puts the cookie back, which is why
  # #156 turns its PUT into a GET.
  test 'the token is emitted where a form needs it, and nowhere else' do
    get '/supported-hardware/featured'
    assert_select 'meta[name=csrf-token]', false,
                  'a page with no form is minting a CSRF token, which writes the session'

    get '/open-wall'
    assert_select 'meta[name=csrf-token]', false
  end

  # Restored after a review: an edit of mine spliced these two out while
  # rewriting the wizard tests, which is how coverage quietly disappears -- the
  # suite went green and nothing named what had stopped being checked.
  #
  # The catalogue JSON is the one endpoint here with consumers nobody in this
  # project controls, so it gets its own policy rather than the hour the
  # catalogue pages around it take: a reader can wait out a stale page, a
  # script polling this cannot tell it is stale.
  test 'the catalogue feed declares its own freshness, not the pages around it' do
    get '/cameras/socs.json'

    assert_response :success
    assert_match(/max-age=300\b/, response.headers['Cache-Control'].to_s,
                 'the feed inherited the catalogue pages\' one-hour lifetime')
    assert_includes response.headers['Cache-Control'].to_s, 'public'
  end

  test 'the catalogue feed answers a conditional request without a body' do
    get '/cameras/socs.json'
    etag = response.headers['ETag']

    assert etag.present?, 'no ETag, so a poller must re-download an unchanged feed every time'

    get '/cameras/socs.json', headers: { 'If-None-Match' => etag }

    assert_response :not_modified
    assert_empty response.body
  end

  # The wizard was the last public page with a token, and therefore the last
  # one setting a cookie. #156 made its form a GET -- the action persisted
  # nothing, so the verb bought nothing -- and a GET form carries no
  # authenticity_token, so the page joins the rest.
  test 'the wizard is cookieless and cacheable now that its form is a GET' do
    vendor = Vendor.find_by(name: 'Cache Test Vendor') || Vendor.create!(name: 'Cache Test Vendor')
    soc = Soc.find_by(model: 'CT1000') ||
          Soc.create!(model: 'CT1000', vendor: vendor, family: 'ct', status: 'done',
                      uboot_filename: 'u.bin', linux_filename: 'l.bin')
    path = "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}"

    get path

    assert_response :success
    assert_nil response.headers['Set-Cookie']
    assert_select 'meta[name=csrf-token]', false,
                  'a GET form needs no token, and minting one would write the session'
    assert_includes response.headers['Cache-Control'].to_s, 'public'

    # And the result it produces is a URL, which is the point of #156.
    get path, params: { camera: { flash_type: 'nor8m', firmware_version: 'lite' } }

    assert_response :success
    assert_nil response.headers['Set-Cookie']
  end
end
