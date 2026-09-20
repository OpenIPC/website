# frozen_string_literal: true

require 'test_helper'

# The locale is part of the address now (#154). Everything here is about the
# one property that makes the rest of the epic possible: the URL determines the
# content, so a shared cache can store it.
class LocaleInPathTest < ActionDispatch::IntegrationTest
  test 'a prefixed path renders in that language' do
    get '/ru/donate'

    assert_response :success
    assert_select 'html[lang=?]', 'ru'
  end

  test 'english keeps the bare path, so no indexed URL moves' do
    get '/donate'

    assert_response :success
    assert_select 'html[lang=?]', 'en'
  end

  test 'every locale has its own address for the same page' do
    %w[/get-started /ru/get-started /zh/get-started].each do |path|
      get path

      assert_response :success, path
    end
  end

  # The point of the whole change: two visitors asking for the same URL get the
  # same page, whatever either of them has in a cookie.
  test 'a prefixed path ignores the session' do
    get '/zh'
    assert_select 'html[lang=?]', 'zh'

    get '/ru/donate'
    assert_select 'html[lang=?]', 'ru' # the path must win over a stored choice
  end

  test 'a prefixed path does not write the session' do
    get '/ru/donate'
    get '/donate'

    assert_select 'html[lang=?]', 'en' # /ru/ must not make later bare paths Russian
  end

  # ?locale= is retired: it now answers 301 to the prefixed path rather than
  # rendering in place, and query_locale_redirect_test covers the rules. What
  # this asserts is that the old links still arrive somewhere correct, which is
  # the promise made to everyone who has one in a bookmark or a forum post.
  test 'an old query-parameter link still arrives in the right language' do
    get '/?locale=ru'

    assert_response :moved_permanently
    follow_redirect!
    assert_select 'html[lang=?]', 'ru'
  end

  # The constraint is what stops (:locale) swallowing the site. An unknown
  # prefix is not a locale, so it falls through to the catch-all like any other
  # unrecognised path rather than rendering the page in a language that has no
  # translation file behind it.
  test 'a locale the site does not serve is not a locale' do
    get '/xx/donate'

    assert_response :redirect
    assert_equal 'http://www.example.com/', response.location
  end

  test 'links inside a prefixed page keep the prefix' do
    get '/ru/donate'

    assert_select 'a[href=?]', '/ru/get-started'
    assert_select 'nav a[href=?]', '/ru/supported-hardware'
  end

  # The catalogue and the snapshot pages were the last two route families
  # without an address of their own, because `scope "(:locale)"` puts :locale
  # first among the dynamic segments and every helper here takes a vendor, a
  # SoC or a snapshot positionally. #154 converted those call sites to keyword
  # form, which is what let these move inside the scope.
  #
  # The catalogue is built rather than looked up. The test database is
  # regenerated from development and carries no vendors or SoCs at all, so a
  # test naming a real model -- rv1106, say -- fails with RecordNotFound for a
  # reason that has nothing to do with locales, and one that skips when the
  # table is empty never runs.
  MINIMAL_JPEG = "\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00\xFF\xD9".b

  def some_soc
    @some_soc ||= begin
      vendor = Vendor.find_by(name: 'Locale Test Vendor') ||
               Vendor.create!(name: 'Locale Test Vendor')
      Soc.find_by(model: 'LT1000') ||
        # uboot_filename and linux_filename are what make it `instructable?`,
        # which is the branch that renders the catalogue link this file
        # asserts on. Without them the partial raises on nil.
        Soc.create!(model: 'LT1000', vendor: vendor, family: 'lt', status: 'done',
                    uboot_filename: 'u-boot-lt1000.bin', linux_filename: 'uImage.lt1000')
    end
  end

  def soc_path_for(prefix)
    "#{prefix}/cameras/vendors/#{some_soc.vendor.to_param}/socs/#{some_soc.to_param}"
  end

  test 'the catalogue has an address in every language' do
    { '/supported-hardware/featured' => 'en',
      '/ru/supported-hardware/featured' => 'ru',
      '/zh/supported-hardware/featured' => 'zh' }.each do |path, lang|
      get path

      assert_response :success
      assert_select 'html[lang=?]', lang
    end
  end

  test 'a SoC page has an address in every language' do
    { '' => 'en', '/ru' => 'ru', '/zh' => 'zh' }.each do |prefix, lang|
      get soc_path_for(prefix)

      assert_response :success
      assert_select 'html[lang=?]', lang
    end
  end

  # A visitor who reaches the catalogue in Russian must not be handed back to
  # English by the next click. This is the invariant that caught the navbar
  # Hardware link dropping people on the English homepage.
  test 'links inside a prefixed catalogue page keep the prefix' do
    some_soc
    get '/ru/supported-hardware/full-list'

    assert_response :success
    assert_select 'a[href^=?]', '/ru/cameras/vendors/', minimum: 1
    assert_select 'a[href^="/cameras/vendors/"]', false
  end

  test 'a snapshot has an address in every language' do
    snapshot = Snapshot.new(mac_address: '00:11:22:33:44:66', ip_address: '203.0.113.10',
                            soc: 'gk7205v300', sensor: 'imx307')
    snapshot.file.attach(io: StringIO.new(MINIMAL_JPEG), filename: 'snapshot.jpg',
                         content_type: 'image/jpeg')
    snapshot.save!(validate: false)

    get "/ru/snapshots/#{snapshot.id}"

    assert_response :success
    assert_select 'html[lang=?]', 'ru'
  end

  # /ru/assets/... and /ru/fonts/... are 404s. The first draft of locale_path
  # produced both by rewriting the font preloads in the layout.
  test 'assets and fonts are never prefixed' do
    get '/ru/donate'

    assert_select 'link[rel=preload][href^=?]', '/fonts/'
    assert_select 'link[rel=stylesheet][href^=?]', '/assets/'
  end

  test 'each page names its translations, and itself' do
    get '/ru/donate'

    assert_select 'link[rel=alternate][hreflang=?][href$=?]', 'en', '/donate'
    assert_select 'link[rel=alternate][hreflang=?][href$=?]', 'ru', '/ru/donate'
    assert_select 'link[rel=alternate][hreflang=?][href$=?]', 'zh', '/zh/donate'
    assert_select 'link[rel=alternate][hreflang=?][href$=?]', 'x-default', '/donate'
  end

  # Pointing all three at the English canonical is what made two thirds of the
  # site unindexable, and is the thing this change exists to end.
  test 'a translated page is its own canonical' do
    get '/ru/donate'

    assert_select 'link[rel=canonical][href$=?]', '/ru/donate'
  end

  test 'the switcher offers the other languages by path' do
    get '/donate'

    assert_select 'a[lang=ru][href=?]', '/ru/donate'
    assert_select 'a[lang=zh][href=?]', '/zh/donate'
    assert_select 'a[lang=en][href=?]', '/donate'
  end

  # The invariant, rather than another individual case.
  #
  # Two links have already been found pointing at routes that do not exist in
  # a prefixed form -- /ru/open-wall in review, and /ru/supported-hardware on
  # dev, which is the navbar's "Hardware" item and sent Russian and Chinese
  # visitors to the ENGLISH homepage. Both were invisible because locale_path
  # will happily prefix a path whether or not anything answers it.
  #
  # So: follow every internal link on a localized page and require it to
  # resolve. A redirect counts as a failure unless it stays in the language --
  # a navigation click that changes the language is the bug, not the fix.
  %w[ru zh].each do |locale|
    test "every internal link on a #{locale} page stays in #{locale}" do
      get "/#{locale}/donate"

      assert_response :success
      # The language switcher deliberately points at the other languages, so it
      # is the one set of links exempt from this. It is the only place that
      # carries a lang attribute.
      links = css_select('a[href^="/"]:not([lang])').map { |a| a['href'] }.uniq
      refute_empty links

      broken = links.filter_map do |href|
        path = href.split('?').first
        # 200 is not enough. A link to the unprefixed /business answers 200 --
        # in English -- so a page can pass a liveness check while quietly
        # sending half its readers into another language. 48 such links live
        # inside the locale YAML, where locale_path cannot see them.
        next if path.start_with?("/#{locale}/") || path == "/#{locale}"
        next if path.match?(%r{\A/(assets|fonts|files|dl|wall|images)/}) ||
                File.extname(path).present?

        get href
        "#{href} -> #{response.status} #{response.redirect? ? response.location : ''}"
      end

      assert_empty broken, <<~MESSAGE.chomp
        Links on /#{locale}/donate that leave the language:

        #{broken.map { |b| "  #{b}" }.join("\n")}

        locale_path prefixes a path whether or not a route answers it, so an
        unscoped route becomes a link to the English homepage.
      MESSAGE
    end
  end
end
