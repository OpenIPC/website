# frozen_string_literal: true

require 'test_helper'

# There was no sitemap: /sitemap.xml fell through to the `*unmatched`
# catch-all and 302'd to the homepage, so every search engine that asked was
# told the site has none and then handed a page (#154).
class SitemapTest < ActionDispatch::IntegrationTest
  setup { get '/sitemap.xml' }

  test 'it is served as xml rather than redirecting to the homepage' do
    assert_response :success
    assert_equal 'application/xml', response.media_type
  end

  test 'every page appears once per locale' do
    locs = response.body.scan(%r{<loc>([^<]+)</loc>}).flatten

    assert_includes locs, 'http://www.example.com/donate'
    assert_includes locs, 'http://www.example.com/ru/donate'
    assert_includes locs, 'http://www.example.com/zh/donate'
    assert_equal locs.uniq, locs, 'a URL listed twice'
  end

  # A set where one member omits itself is not a set, and search engines
  # discard the lot.
  test 'each entry names every translation including itself' do
    entry = response.body[%r{<url>\s*<loc>http://www\.example\.com/ru/donate</loc>.*?</url>}m]

    assert entry, 'the Russian donate page is not in the sitemap'
    %w[en ru zh x-default].each do |code|
      assert_includes entry, %(hreflang="#{code}"), "#{code} alternate missing"
    end
    assert_includes entry, 'href="http://www.example.com/ru/donate"'
  end

  # The routes also contain the admin area, the API, thirty-odd redirects and
  # two 410s. A sitemap that offers any of those is worse than none.
  test 'it offers nothing that is not a public page' do
    locs = response.body.scan(%r{<loc>([^<]+)</loc>}).flatten

    %w[/admin /up /snapshots /binaries /cameras/socs.json].each do |forbidden|
      assert_empty locs.grep(/#{Regexp.escape(forbidden)}/), "#{forbidden} is in the sitemap"
    end
  end

  # The one that matters, and the one that was missing.
  #
  # The first version of this listed the 126-page hardware catalogue in three
  # languages. /ru/cameras/... is not a route -- the catalogue helpers take a
  # vendor and a SoC positionally and :locale would swallow the first -- so the
  # sitemap advertised 252 URLs that 302 to the English homepage. Telling a
  # search engine to crawl a redirect is worse than telling it nothing.
  test 'every URL it advertises actually renders' do
    locs = response.body.scan(%r{<loc>http://www\.example\.com(/[^<]*)</loc>}).flatten.uniq
    refute_empty locs

    redirecting = locs.reject do |path|
      get path
      response.successful?
    end

    assert_empty redirecting, <<~MESSAGE.chomp
      The sitemap offers URLs that do not render:

      #{redirecting.first(10).map { |p| "  #{p}" }.join("\n")}

      A search engine told to crawl a redirect is worse off than one told
      nothing. Either localize the route or leave it out of the sitemap.
    MESSAGE
  end
end
