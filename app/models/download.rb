# frozen_string_literal: true

# A firmware image that was sent. See issue #83: nothing recorded this before,
# and nginx keeps fourteen days, so there has never been a figure for a month.
class Download < ApplicationRecord
  # soc_model names the chip. soc_id is left in the table and no longer
  # written: it was a row id in `socs`, which is not a table any more (#289),
  # and the ids it holds already meant nothing without that table. It goes
  # with the move to PostgreSQL (#293).

  # Only created, never updated, so there is no updated_at to maintain.
  self.record_timestamps = false

  # What a row means changed on 2026-09-21 (#188).
  #
  # Before that date a row was one HTTP request, and a chunked or resumed fetch
  # wrote several -- so the table counted requests, not downloads, and counted
  # the large images worst. From that date only the first chunk of a fetch is
  # recorded, which is the download.
  #
  # The 2,558 rows written before it are left exactly as they are. Rewriting
  # history would make the table agree with itself and disagree with the nginx
  # log, which is the only independent record of what actually happened. A
  # chart that crosses this date is comparing two different measurements, and
  # should say so.
  COUNTS_ONE_ROW_PER_DOWNLOAD_FROM = Date.new(2026, 9, 21)

  # Recording must never cost somebody their download. A full disk, a locked
  # table, a migration not yet run on one container -- none of those are
  # reasons to fail a request that has already produced a valid image, so this
  # logs and carries on.
  def self.record(firmware:, soc:, bytes: nil)
    create!(soc_model: soc.model_downcase, flash_type: firmware.flash_type,
            release: firmware.release, flash_size: firmware.flash_size,
            bytes: bytes, created_at: Time.current)
  rescue StandardError => e
    # Loud, because a rescue this broad will otherwise hide a plain bug: this
    # once returned nil for every call during development because of a missing
    # association, and the only sign was a nil where a row should have been.
    Rails.logger.error "download not recorded for #{soc.model_downcase}: #{e.class}: #{e.message}"
    nil
  end
end
