# frozen_string_literal: true

require 'test_helper'

class MultilangTest < ActionDispatch::IntegrationTest
  # The switcher marks the language it is currently rendering in, so the page
  # says which locale won without needing a translated string to compare.
  def assert_rendering_in(locale)
    assert_response :success
    assert_match %(<a lang="#{locale}" class="fw-bold" aria-current="true"), response.body
  end

  def get_root(accept_language: nil, params: {})
    headers = accept_language ? { 'HTTP_ACCEPT_LANGUAGE' => accept_language } : {}
    get '/', params:, headers:
  end

  # LOCALES fed the switcher while available_locales fed everything else, and
  # they disagreed: LOCALES carried `ja`, which had no locale file and was never
  # available, so the switcher offered a language the app could not render.
  test 'the switcher offers exactly the locales the app can render' do
    assert_equal I18n.available_locales.sort, Multilang::LOCALES.keys.sort
  end

  test 'three languages, no more' do
    assert_equal %i[en ru zh], I18n.available_locales.sort
  end

  test 'the switcher is in the page' do
    get_root

    assert_response :success
    Multilang::LOCALES.each_value { |name| assert_match name, response.body }
  end

  # The icon was a hardcoded <img src="/assets/translate.svg">. Production
  # serves fingerprinted assets with compile off, so that is a 404 on every
  # page -- and the switcher is in the layout, so on every page of the site.
  test 'the switcher icon goes through the asset pipeline' do
    get_root

    assert_match %r{src="/assets/translate-[0-9a-f]{64}\.svg"}, response.body
    assert_no_match %r{src="/assets/translate\.svg"}, response.body
  end

  # Every link was a bare "?locale=xx", which replaces the whole query string.
  # Switching language on a paginated list lost the page, and on a permanent
  # link to a camera configuration it lost the configuration.
  test 'switching language keeps the rest of the query string' do
    get '/supported-hardware/featured?vendor=hisilicon&locale=en'

    assert_response :success
    assert_match 'href="/supported-hardware/featured?locale=ru&amp;vendor=hisilicon"', response.body
  end

  # aria-labelledby named "dropdownLanguage" while the button was
  # id="dropsownLanguage", so it labelled nothing at all.
  test 'the dropdown label points at the button that opens it' do
    get_root

    assert_match 'id="dropdownLanguage"', response.body
    assert_no_match(/dropsownLanguage/, response.body)
  end

  # --- what the browser asks for ---

  # The old scan only matched a tag followed by `;`, so the first entry in the
  # header -- the visitor's top preference, which carries no q-value -- was the
  # one it could never see. This header answered `en` for a Russian speaker.
  test 'the first Accept-Language entry is the one that wins' do
    get_root accept_language: 'ru,en;q=0.9'

    assert_rendering_in 'ru'
  end

  test 'a q-value still orders the rest' do
    get_root accept_language: 'de-DE,zh;q=0.8,en;q=0.9'

    assert_rendering_in 'en'
  end

  test 'a language this site does not serve falls through to one it does' do
    get_root accept_language: 'de,zh;q=0.9'

    assert_rendering_in 'zh'
  end

  # q=0 means "not acceptable" (RFC 9110 12.4.2). Keeping those entries let a
  # language the client had explicitly ruled out win, just because nothing
  # better was on offer.
  test 'a language the visitor ruled out is not chosen anyway' do
    get_root accept_language: 'ru;q=0,de;q=0.9'

    assert_rendering_in 'en'
  end

  # `;q=` was matched as a literal, and the grammar allows whitespace around
  # the separator. This header ranked English at 1 and picked it -- the
  # opposite of what the visitor asked for.
  test 'a space before the q-value does not turn it into a preference' do
    get_root accept_language: 'en; q=0.1,ru;q=0.9'

    assert_rendering_in 'ru'
  end

  # And the parameter name is case-insensitive. Without that, zh would rank at
  # the default 1 and beat the en it is explicitly ranked below.
  test 'an uppercase Q is a q-value too' do
    get_root accept_language: 'de-DE,zh;Q=0.8,en;q=0.9'

    assert_rendering_in 'en'
  end

  test 'a header with nothing we serve gets the default' do
    get_root accept_language: 'de-DE,de;q=0.9,fr;q=0.8'

    assert_rendering_in 'en'
  end

  test 'no header at all gets the default' do
    get_root

    assert_rendering_in 'en'
  end

  # --- what the switcher asks for ---

  test 'the switcher changes the language and it sticks' do
    get_root params: { locale: 'zh' }
    assert_rendering_in 'zh'

    get_root

    assert_rendering_in 'zh'
  end

  # de was served until this change. A bookmarked ?locale=de must not leave
  # I18n.locale pointing at a language with no translation file behind it.
  test 'a locale that is no longer served is ignored, not honoured' do
    get_root params: { locale: 'de' }

    assert_rendering_in 'en'
  end

  test 'a nonsense locale is ignored too' do
    get_root params: { locale: '../../etc/passwd' }

    assert_rendering_in 'en'
  end

  # The layout already interpolates I18n.locale into the lang attribute, which
  # only ever said "en" while set_locale was off. It matters now: screen readers
  # pick a voice from it and search engines index by it.
  test 'the page declares the language it is actually in' do
    get_root params: { locale: 'zh' }

    assert_match '<html dir="ltr" lang="zh">', response.body
  end

  # --- keys that were missing rather than untranslated ---

  # The controller asked for pages.qr_code.title; the key is
  # pages.qr_code_generator.title. Nothing had that name in any locale, so the
  # browser tab has been reading "translation missing: en.pages.qr_code.title".
  test 'the QR generator page has a title rather than a missing-translation notice' do
    get '/tools/qr-code-generator'

    assert_response :success
    assert_match '<title>Wireless Network QR Code Generator - OpenIPC</title>', response.body
    assert_no_match(/translation missing/i, response.body)
  end

  # site.snapshot.view_heif existed only as an inline English default in the
  # view, so it read as English on a Russian page and i18n-tasks could not see
  # it was untranslated.
  test 'the HEIF button is a translation, not an inline English default' do
    ru = I18n.t('site.snapshot.view_heif', locale: :ru)

    assert_equal 'View original HEIF in your browser', I18n.t('site.snapshot.view_heif', locale: :en)
    assert_no_match(/translation missing/i, ru)
    assert_not_equal I18n.t('site.snapshot.view_heif', locale: :en), ru
  end

  # --- Russian counts ---

  # Russian has four plural forms and I18n's default pluralizer knows two, so
  # every count from 2 up took the `other` string: "5 ошибки" where Russian
  # wants "5 ошибок", and "21 ошибки" where it wants "21 ошибка". Adding the
  # forms to the locale file does nothing on its own -- lib/locale/plurals.rb
  # is what selects them.
  test 'Russian error counts pick the right one of four forms' do
    said = ->(n) { I18n.t('errors.messages.not_saved', count: n, resource: 'запись', locale: :ru) }

    assert_match(/\bошибка не позволила\b/, said[1])
    assert_match(/\bошибка не позволила\b/, said[21])
    assert_match(/\bошибки не позволили\b/, said[2])
    assert_match(/\bошибки не позволили\b/, said[22])
    assert_match(/\bошибок не позволили\b/, said[5])
    assert_match(/\bошибок не позволили\b/, said[11])
  end

  test 'English counts are unaffected by the Russian rule' do
    said = ->(n) { I18n.t('errors.messages.not_saved', count: n, resource: 'record', locale: :en) }

    assert_match(/1 error prohibited/, said[1])
    assert_match(/5 errors prohibited/, said[5])
  end

  # I18n.locale is per-thread and nothing resets it after a request, so
  # set_locale wraps the action in with_locale rather than assigning.
  test 'the request does not leave the process in another language' do
    get_root params: { locale: 'ru' }

    assert_rendering_in 'ru'
    assert_equal :en, I18n.locale
  end
end
