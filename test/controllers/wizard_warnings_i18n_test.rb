# frozen_string_literal: true

require 'test_helper'

# The wizard's safety messages, in the visitor's language (#163).
#
# These were English sentences built in Ruby inside Cameras::SocsController, so
# a Russian reader following the instructions was told in English that the
# commands in front of them could not produce a working camera -- the one
# sentence on that page it is least safe to miss.
#
# They are keys now, which is also what #163's export carries: a key and its
# arguments travel between the two halves of the site, a rendered sentence in
# one language does not.
class WizardWarningsI18nTest < ActionDispatch::IntegrationTest
  KEYS = %w[
    edition_not_published eight_meg_chip eight_meg_layout layout_changed
    no_lite_chip no_lite_layout nothing_published
  ].freeze

  test 'every warning exists in every locale' do
    I18n.available_locales.each do |locale|
      KEYS.each do |key|
        value = I18n.t("cameras.socs.warnings.#{key}", locale: locale, default: nil)

        assert value.present?, "#{locale} has no cameras.socs.warnings.#{key}"
        assert_kind_of String, value
      end
    end
  end

  test 'the interpolations are the same in every locale' do
    # A translation that drops %{flash} renders a sentence missing the one
    # fact that made it worth showing, and i18n raises on an argument the
    # string does not take.
    KEYS.each do |key|
      wanted = I18n.t("cameras.socs.warnings.#{key}", locale: :en).scan(/%\{(\w+)\}/).flatten.sort

      I18n.available_locales.each do |locale|
        found = I18n.t("cameras.socs.warnings.#{key}", locale: locale).scan(/%\{(\w+)\}/).flatten.sort

        assert_equal wanted, found,
                     "#{locale}'s #{key} interpolates #{found.inspect}, English #{wanted.inspect}"
      end
    end
  end

  test 'a Russian visitor is warned in Russian' do
    # The bug this fixes, asserted through the page rather than the catalogue:
    # an 8MB chip asked for Ultimate warns, and the warning was English for
    # everyone.
    vendor = Vendor.create!(name: 'Testco')
    soc = Soc.create!(vendor: vendor, model: 'TS3516EV300', status: 'done',
                      uboot_filename: '', linux_filename: '')

    get "/ru/cameras/vendors/#{vendor.urlname}/socs/#{soc.urlname}",
        params: { camera: { flash_type: 'nor8m', partition_layout: 'nor8m',
                            firmware_version: 'ultimate', network_interface: 'eth',
                            sd_card_slot: 'nosd' } }

    assert_response :success
    body = response.body
    russian = I18n.t('cameras.socs.warnings.eight_meg_chip', locale: :ru)
    english = I18n.t('cameras.socs.warnings.eight_meg_chip', locale: :en)

    if body.include?(english)
      flunk "the Russian page shows the English warning: #{english}"
    end

    assert_includes body, russian, 'the Russian page shows no 8MB warning at all'
  end
end
