# frozen_string_literal: true

require 'test_helper'

# The <head> is the part of a page nobody looks at, so it is the part that
# silently rots. These are the assertions that would have caught each way it
# has gone wrong before.
class PageMetadataTest < ActionDispatch::IntegrationTest
  test 'every page carries a description' do
    get '/'

    assert_response :success
    assert_select 'meta[name="description"]' do |tags|
      assert_equal 1, tags.size, 'exactly one description, or crawlers pick arbitrarily'
      assert_predicate tags.first['content'].to_s, :present?
    end
  end

  # The description is translated, so a locale that has not been given the key
  # would render the English one -- fine -- or, without fallbacks, a
  # "translation missing" span, which would be published into search results.
  test 'the description is written in the locale being rendered' do
    get '/?locale=ru'

    ru = css_select('meta[name="description"]').first['content']

    get '/?locale=en'

    en = css_select('meta[name="description"]').first['content']

    assert_not_equal en, ru
    [en, ru].each { |text| assert_no_match(/translation missing/i, text) }
  end

  # ?locale=ru is a rendering of the same page, not a different one. If the
  # query string reached the canonical, each translation would compete with the
  # others for the same content.
  test 'the canonical URL drops the query string' do
    get '/supported-hardware/featured?locale=ru&page=2'

    canonical = css_select('link[rel="canonical"]').first['href']

    assert_equal '/supported-hardware/featured', URI.parse(canonical).path
    assert_nil URI.parse(canonical).query
  end

  # Crawlers do not resolve a relative og:image, and a page whose preview image
  # 404s is shared without one at all.
  test 'og:image is absolute' do
    get '/'

    src = css_select('meta[property="og:image"]').first['content']

    assert_match %r{\Ahttps?://}, src
    assert_equal '/og-default.png', URI.parse(src).path
  end

  test 'the og:image exists in public' do
    assert_path_exists Rails.public_path.join('og-default.png')
  end

  # The preloaded faces have to be the ones the stylesheet asks for; a typo here
  # costs a download rather than saving one, and nothing else would notice.
  test 'preloaded fonts exist and are the ones the stylesheet declares' do
    get '/'

    hrefs = css_select('link[rel="preload"][as="font"]').map { |tag| tag['href'] }

    assert_not_empty hrefs
    stylesheet = Rails.root.join('app/assets/stylesheets/_fonts.scss').read
    hrefs.each do |href|
      assert_path_exists Rails.public_path.join(href.delete_prefix('/'))
      # _fonts.scss builds the filenames by interpolation, so match the parts.
      parts = href.match(%r{ibm-plex-sans-(?<subset>.+)-(?<weight>\d+)-normal\.woff2\z})

      assert_not_nil parts, "#{href} is not a name _fonts.scss can generate"
      assert_includes stylesheet, "'#{parts[:subset]}'"
      assert_includes stylesheet, parts[:weight]
    end
  end

  # Pages that lay out their own full-bleed sections opt out of the wrapper.
  # Existing pages set nothing and must keep it.
  test 'pages that do not ask for full width keep the container wrapper' do
    get '/introduction'

    assert_select 'main > div.container'
  end
end
