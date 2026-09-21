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

  test 'the retention the page promises is the retention the job applies' do
    days = PurgeImagesJob::RETENTION.in_days.to_i

    LOCALES.each do |locale|
      assert_includes claim(:wall_retention_html, locale), days.to_s, <<~MESSAGE.chomp
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
    latest = PurgeImagesJob::RETENTION.in_days.to_i + 1

    LOCALES.each do |locale|
      assert_includes claim(:wall_retention_html, locale), latest.to_s,
                      "the #{locale} page does not say #{latest} as the outside case"

      %i[row_images_long row_details_long].each do |row|
        assert_includes claim(row, locale), latest.to_s,
                        "the #{locale} retention table disagrees with the prose above it"
      end
    end
  end

  test 'the upload interval the page states is the one the model enforces' do
    minutes = Snapshot::INTERVAL_LIMIT.in_minutes.to_i

    LOCALES.each do |locale|
      assert_includes claim(:wall_withheld_html, locale), minutes.to_s,
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
      assert_includes claim(:log_text_html, locale), '14',
                      "the #{locale} page no longer states the log retention at all"
    end
  end
end
