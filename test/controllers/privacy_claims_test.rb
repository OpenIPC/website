# frozen_string_literal: true

require 'test_helper'

# The privacy page states retention periods and limits as prose, in three
# languages. Nothing makes that prose follow the code, and the failure is
# silent in the worst possible direction: the page keeps rendering, keeps
# reading as a promise, and is wrong.
#
# It is also the page where being wrong matters most. Every figure on it was
# checked against the running system when it was written (#182); this file is
# what keeps that true after the constant moves.
class PrivacyClaimsTest < ActiveSupport::TestCase
  LOCALES = %i[en ru zh].freeze

  def claim(key, locale)
    I18n.t("pages.privacy.#{key}", locale: locale)
  end

  # Whole numbers, not substrings. assert_includes("... 12 days ...", "2")
  # passes, so a page that had drifted to 12 days would have satisfied a check
  # for 2 -- which is the one thing this file exists to catch.
  def figures(key, locale)
    claim(key, locale).scan(/\d+/)
  end

  # The page states whole days and whole minutes. If a constant stops being
  # one -- 60.hours, 15.5.minutes -- flooring it would let the old wording
  # stand while the system does something else, so this fails instead and
  # makes the copy a decision rather than a rounding.
  def whole(duration, unit)
    value = duration.public_send("in_#{unit}")
    assert_equal value.to_i, value, <<~MESSAGE.chomp
      #{duration.inspect} is not a whole number of #{unit}, and the privacy
      page states this figure in #{unit}.

      Either express the constant in whole #{unit} or rewrite the sentence in
      all three locales -- do not let it round.
    MESSAGE
    value.to_i
  end

  test 'the retention the page promises is the retention the job applies' do
    days = whole(PurgeImagesJob::RETENTION, :days)

    LOCALES.each do |locale|
      assert_includes figures(:wall_retention_html, locale), days.to_s, <<~MESSAGE.chomp
        The #{locale} privacy page does not say #{days} days, but
        PurgeImagesJob::RETENTION is #{PurgeImagesJob::RETENTION.inspect}.

        Whoever changed the constant has left the page promising the old one,
        in a language they may not read.
      MESSAGE
    end
  end

  # The constant is an eligibility cutoff, not a deletion time: the sweep is a
  # nightly cron, so an upload that lands just after one run waits for the next
  # and can reach RETENTION + 1 day. The page says so rather than rounding in
  # its own favour, and the table's figure has to move with the constant too.
  test 'the outside figure allows for the sweep being nightly' do
    latest = whole(PurgeImagesJob::RETENTION, :days) + 1

    LOCALES.each do |locale|
      assert_includes figures(:wall_retention_html, locale), latest.to_s,
                      "the #{locale} page does not say #{latest} as the outside case"

      %i[row_images_long row_details_long].each do |row|
        assert_equal [latest.to_s], figures(row, locale),
                     "the #{locale} retention table disagrees with the prose above it"
      end
    end
  end

  test 'the upload interval the page states is the one the model enforces' do
    minutes = whole(Snapshot::INTERVAL_LIMIT, :minutes)

    LOCALES.each do |locale|
      assert_includes figures(:wall_withheld_html, locale), minutes.to_s,
                      "the #{locale} page does not say #{minutes} minutes"
    end
  end

  # What this file cannot check, said out loud so the gap is visible rather
  # than assumed covered.
  #
  # The page also promises the server log is deleted after fourteen days. That
  # number is `rotate 14` in /etc/logrotate.d/nginx on the origin -- Debian's
  # stock file, not in this repository -- so a rebuilt host takes whatever the
  # distribution ships and nothing here would notice a change. Bringing that
  # file in, as #144 did for the nginx configuration, is what would close it.
  #
  # The counter's own settings are the same shape of gap: what it collects
  # lives in its database, not in this tree.
  test 'the claims that cannot be checked here are still made' do
    LOCALES.each do |locale|
      assert_includes figures(:log_text_html, locale), '14',
                      "the #{locale} page no longer states the log retention at all"
    end
  end
end
