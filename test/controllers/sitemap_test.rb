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

  test 'the hardware catalogue is included, because it is what people search for' do
    soc = Soc.includes(:vendor).find(&:vendor)

    skip 'no SoC fixtures' if soc.nil?
    assert_includes response.body, "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}"
  end
end
