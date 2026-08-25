# frozen_string_literal: true

require 'test_helper'

class MultilangTest < ActionDispatch::IntegrationTest
  # The switcher marks the language it is currently rendering in, so the page
  # says which locale won without needing a translated string to compare.
  def assert_rendering_in(locale)
    assert_response :success
    assert_match %(<a href="?locale=#{locale}" class=" fw-bold">), response.body
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

  # I18n.locale is per-thread and nothing resets it after a request, so
  # set_locale wraps the action in with_locale rather than assigning.
  test 'the request does not leave the process in another language' do
    get_root params: { locale: 'ru' }

    assert_rendering_in 'ru'
    assert_equal :en, I18n.locale
  end
end
