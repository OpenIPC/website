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

  # The routes also contain the API, thirty-odd redirects and a handful of
  # 410s, the retired admin among them. A sitemap that offers any of those is worse than none.
  test 'it offers nothing that is not a public page' do
    locs = response.body.scan(%r{<loc>([^<]+)</loc>}).flatten

    %w[/admin /up /snapshots /binaries /cameras/socs.json].each do |forbidden|
      assert_empty locs.grep(/#{Regexp.escape(forbidden)}/), "#{forbidden} is in the sitemap"
    end
  end

  # The one that matters, and the one that was missing.
  #
  # The first version of this listed the 126-page hardware catalogue in three
  # languages before /ru/cameras/... was a route -- the catalogue helpers took
  # a vendor and a SoC positionally and :locale swallowed the first -- so the
  # sitemap advertised 252 URLs that 302'd to the English homepage. Telling a
  # search engine to crawl a redirect is worse than telling it nothing. #154
  # localized those routes, so the catalogue is listed now, and this is what
  # keeps it honest.
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

# The catalogue is the part of the site with real long-tail search value and
# the reason the sitemap sat half-finished for so long. Each test starts from
# an empty catalogue, so these build one -- otherwise every assertion about the
# catalogue passes over an empty list.
class SitemapCatalogueTest < ActionDispatch::IntegrationTest
  setup do
    vendor = Vendor.create!(name: 'Sitemap Test Vendor')
    @soc = Soc.create!(model: 'SM2000', vendor: vendor, family: 'sm', status: 'done',
                       uboot_filename: 'u-boot-sm2000.bin', linux_filename: 'uImage.sm2000')
    get '/sitemap.xml'
  end

  def locs
    response.body.scan(%r{<loc>http://www\.example\.com(/[^<]*)</loc>}).flatten
  end

  test 'every SoC is offered in every language' do
    path = "/cameras/vendors/#{@soc.vendor.to_param}/socs/#{@soc.to_param}"

    assert_includes locs, path
    assert_includes locs, "/ru#{path}"
    assert_includes locs, "/zh#{path}"
  end

  test 'every vendor is offered in every language' do
    path = "/cameras/vendors/#{@soc.vendor.to_param}"

    assert_includes locs, path
    assert_includes locs, "/ru#{path}"
    assert_includes locs, "/zh#{path}"
  end

  # /cameras/socs without ?vendor= redirects to the featured page, which is
  # already listed. Offering both tells a crawler to follow a hop to a URL it
  # already has.
  test 'it does not offer the redirecting catalogue index' do
    assert_empty locs.grep(%r{\A(/(ru|zh))?/cameras/socs\z})
  end

  # The catalogue refuses a slug like this on load, so this is one assigned
  # around that check. Interpolated raw into a path, a value holding "/" or
  # "?" splits one entry into extra segments or a query -- advertising URLs the
  # catalogue routes, which take one segment per identifier, cannot serve. The
  # route helpers escape it.
  test 'a slug with path characters in it cannot break out of its segment' do
    vendor = Vendor.create!(name: 'Escape Probe Vendor')
    Soc.create!(model: 'ESC1', vendor: vendor, family: 'e', status: 'done')
    vendor.urlname = 'escape/probe?x=1'

    get '/sitemap.xml'

    assert_response :success
    assert_not_includes response.body, '/cameras/vendors/escape/probe?x=1'
  end

  test 'the catalogue URLs it advertises render' do
    catalogue = locs.grep(%r{/cameras/vendors/}).uniq

    refute_empty catalogue
    broken = catalogue.reject do |path|
      get path
      response.successful?
    end

    assert_empty broken, "the sitemap offers catalogue URLs that do not render: #{broken.first(5).inspect}"
  end

  # The footer is the site's own statement of which pages matter, and it is on
  # every page. A page reachable from it but absent from the sitemap is one the
  # site links and does not want found, which is never deliberate -- /privacy
  # was in the footer from the day it shipped (#225) and in the sitemap only
  # nine commits later.
  #
  # Read from the rendered page rather than from the template source. The first
  # version of this scanned _footer.html.erb for locale_path('...') with a
  # regex, which meant a link rewritten to double quotes, to link_to, or built
  # from a variable would quietly leave the expected set while the surviving
  # links kept the test green -- a coverage loss that looks exactly like a pass.
  # What a visitor is served is the thing with the obligation, so that is what
  # is asked.
  test 'every page the footer links to is advertised' do
    get '/'

    linked = css_select('footer a')
             .map { |a| a['href'].to_s }
             .select { |href| href.start_with?('/') }
             .map { |href| href.split(/[?#]/).first }
             .uniq

    assert_includes linked, '/privacy', 'the footer stopped linking the privacy page'
    assert_operator linked.size, :>, 10, 'the footer lost most of its links; this test now proves little'

    get '/sitemap.xml'
    paths = response.body.scan(%r{<loc>http://www\.example\.com(/[^<]*)</loc>}).flatten

    # Prefix match: the footer's /supported-hardware redirects to
    # /supported-hardware/featured, which the sitemap correctly lists instead.
    missing = linked.reject { |path| paths.any? { |loc| loc == path || loc.start_with?("#{path}/") } }

    assert_empty missing, <<~MESSAGE.chomp
      The footer links to these, and the sitemap does not offer them:

        #{missing.join("\n        ")}

      Add them to SitemapsController::PAGES, or stop linking them.
    MESSAGE
  end

  # A page with no <title> is worse in the sitemap than out of it: the crawler
  # is invited, and what it indexes is " - OpenIPC". `page_title` joins
  # [@page_title, 'OpenIPC'], so an action that forgets the assignment -- or a
  # route that has no action at all, which is how /privacy shipped in #225 --
  # produces exactly that, silently, in every locale.
  #
  # Walked over the listed pages rather than the catalogue: each of these is a
  # hand-written action that has to remember, where the 126 catalogue pages
  # share one. Three locales, because a title key is per-locale, so English
  # being right says nothing about the other two.
  #
  # "translation missing" is checked separately from emptiness because it is
  # not empty. A deleted locale key renders
  # "translation missing: ru.pages.privacy.title", which would satisfy any test
  # that only asks for non-blank text -- and would then be published as the
  # <title> and the og:title, which is worse than a bare site name.
  # page_metadata_test makes the same distinction for the description.
  test 'every page it advertises has a title of its own' do
    untitled = []
    untranslated = []
    broken = []

    SitemapsController::PAGES.each do |path|
      %w[/ /ru /zh].each do |prefix|
        url = prefix == '/' ? path : "#{prefix}#{path}".chomp('/')
        get url

        next broken << url unless response.successful?

        title = response.body[%r{<title>(.*?)</title>}m, 1].to_s
        next untranslated << url if title.match?(/translation missing/i)

        untitled << url if title.sub(/-\s*OpenIPC\z/, '').strip.empty?
      end
    end

    assert_empty broken, "the sitemap offers pages that do not render: #{broken.inspect}"

    assert_empty untranslated, <<~MESSAGE.chomp
      These pages would publish Rails' placeholder as their title:

        #{untranslated.join("\n        ")}

      The locale is missing the title key. i18n-tasks finds it.
    MESSAGE

    assert_empty untitled, <<~MESSAGE.chomp
      These pages render with no title but "- OpenIPC":

        #{untitled.join("\n        ")}

      Set @page_title in the action. If the route has no action of its own,
      add one -- Rails renders the template without it and nothing notices.
    MESSAGE
  end
end
