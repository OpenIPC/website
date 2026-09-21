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
# the reason the sitemap sat half-finished for so long. It is read from the
# database, and this database carries no vendors or SoCs, so these build one --
# otherwise every assertion about the catalogue passes over an empty list.
class SitemapCatalogueTest < ActionDispatch::IntegrationTest
  setup do
    vendor = Vendor.find_by(name: 'Sitemap Test Vendor') ||
             Vendor.create!(name: 'Sitemap Test Vendor')
    @soc = Soc.find_by(model: 'SM2000') ||
           Soc.create!(model: 'SM2000', vendor: vendor, family: 'sm', status: 'done',
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

  # belongs_to :vendor is required at the model, so this should not arise --
  # but the table has no foreign key to enforce it, and the cost of being
  # wrong is the whole sitemap answering 500 rather than one chip missing
  # from it.
  test 'a chip whose vendor has gone is skipped, not raised on' do
    vendor = Vendor.create!(name: 'Orphan Probe Vendor')
    orphan = Soc.create!(model: 'ORPH1', vendor: vendor, family: 'o', status: 'done',
                         uboot_filename: 'u.bin', linux_filename: 'l.bin')
    Vendor.where(id: vendor.id).delete_all

    get '/sitemap.xml'

    assert_response :success
    assert_not_includes response.body, orphan.urlname
  end

  # urlname is a free text column an admin can edit. Interpolated raw into a
  # path, a value holding "/" or "?" splits one entry into extra segments or a
  # query -- advertising URLs the catalogue routes, which take one segment per
  # identifier, cannot serve. The route helpers escape it.
  test 'a slug with path characters in it cannot break out of its segment' do
    vendor = Vendor.create!(name: 'Escape Probe Vendor')
    vendor.update_column(:urlname, 'escape/probe?x=1')

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
  # was in the footer from the day it shipped (#225) and in the sitemap from
  # nine commits later. Reading the footer rather than listing paths here means
  # the next page added to it is covered without anyone remembering to.
  #
  # Prefix match, because the footer's /supported-hardware is a redirect to
  # /supported-hardware/featured and the sitemap correctly lists the target.
  test 'every page the footer links to is advertised' do
    linked = File.read(Rails.root.join('app/views/layouts/_footer.html.erb'))
                 .scan(%r{locale_path\('(/[^']*)'}).flatten.uniq
    refute_empty linked, 'the footer stopped using locale_path; this test now proves nothing'

    paths = response.body.scan(%r{<loc>http://www\.example\.com(/[^<]*)</loc>}).flatten

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
  test 'every page it advertises has a title of its own' do
    untitled = []
    broken = []

    SitemapsController::PAGES.each do |path|
      %w[/ /ru /zh].each do |prefix|
        url = prefix == '/' ? path : "#{prefix}#{path}".chomp('/')
        get url

        next broken << url unless response.successful?

        title = response.body[%r{<title>(.*?)</title>}m, 1].to_s
        untitled << url if title.sub(/-\s*OpenIPC\z/, '').strip.empty?
      end
    end

    assert_empty broken, "the sitemap offers pages that do not render: #{broken.inspect}"
    assert_empty untitled, <<~MESSAGE.chomp
      These pages render with no title but "- OpenIPC":

        #{untitled.join("\n        ")}

      Set @page_title in the action. If the route has no action of its own,
      add one -- Rails renders the template without it and nothing notices.
    MESSAGE
  end
end
