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
    get '/?locale=zh'
    assert_select 'html[lang=?]', 'zh'

    get '/ru/donate'
    assert_select 'html[lang=?]', 'ru' # the path must win over a stored choice
  end

  test 'a prefixed path does not write the session' do
    get '/ru/donate'
    get '/donate'

    assert_select 'html[lang=?]', 'en' # /ru/ must not make later bare paths Russian
  end

  # Until #155 retires it. Removing this before the prefixed URLs are the ones
  # being linked would give a ?locale= visitor one page in their language and
  # then English for the rest of the visit.
  test 'the old query parameter still works and still sticks' do
    get '/?locale=ru'
    assert_select 'html[lang=?]', 'ru'

    get '/donate'
    assert_select 'html[lang=?]', 'ru' # the session still carries it for unprefixed paths
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
