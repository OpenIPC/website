# frozen_string_literal: true

require 'test_helper'

# The homepage headline stopped naming the category in September 2026. It used
# to read "The open platform for embedded video" -- literal, and carrying the
# words somebody types into a search box. It now reads "The open operating
# system for the machines that see", which is the claim we want to make and is
# not a phrase anyone searches for.
#
# That trade only works while the words it gave up are still on the page
# somewhere search engines weigh: the lede, the <title> and the description.
# Nothing about the rendered page looks wrong if they are edited away one at a
# time, which is why they are asserted here rather than left to a reviewer.
class HomepagePositioningTest < ActionDispatch::IntegrationTest
  # The claim itself, in each language the homepage is written in. A headline
  # that is translated back into "platform for embedded video" has undone the
  # decision, not tidied it.
  OS_CLAIM = {
    'en' => 'operating system',
    'ru' => 'операционная система',
    'zh' => '操作系统'
  }.freeze

  OS_CLAIM.each do |locale, claim|
    test "the #{locale} headline claims an operating system" do
      get locale == 'en' ? '/' : "/#{locale}"

      assert_response :success
      assert_select 'h1', count: 1 do |tags|
        assert_match(/#{Regexp.escape(claim)}/i, tags.first.text,
                     "the #{locale} homepage h1 no longer makes the operating-system claim")
      end
    end
  end

  # What the headline gave up. "Machines that see" is memorable and invisible to
  # search; these are the words a person actually types, and the lede is now the
  # first place on the page they appear.
  test 'the lede still names the hardware the headline no longer does' do
    get '/'

    lede = css_select('.hero-lede').first.text

    ['IP cameras', 'FPV drones', 'robots'].each do |term|
      assert_match(/#{Regexp.escape(term)}/i, lede,
                   "#{term} left the homepage lede, and the headline does not say it either")
    end
  end

  # The <title> was deliberately NOT changed with the headline: it is weighted
  # heavily by search, and "embedded video" is the category name. A later pass
  # that makes the title match the h1 "for consistency" would be the expensive
  # kind of consistency.
  test 'the title and description still name the category' do
    get '/'

    assert_select 'title' do |tags|
      assert_match(/embedded video/i, tags.first.text,
                   'the <title> stopped naming the category; see this test for why it kept it')
    end

    description = css_select('meta[name="description"]').first['content']

    assert_match(/IP cameras/i, description)
  end

  # The lede grew from 141 characters to 184 to carry "the eyes of the physical
  # world". It is set at max-width: 60ch, so it is already four lines on a
  # phone; past roughly 200 it starts to push the buttons below the fold on a
  # 375px screen, which is where most of this page's traffic reads it.
  test 'the lede stays short enough to read on a phone' do
    get '/'

    lede = css_select('.hero-lede').first.text.strip

    assert_operator lede.length, :<=, 200,
                    'the homepage lede is long enough to push the call to action off a phone screen'
  end
end
