# frozen_string_literal: true

# A firmware image that was sent. See issue #83: nothing recorded this before,
# and nginx keeps fourteen days, so there has never been a figure for a month.
class Download < ApplicationRecord
  # Optional, and no foreign key behind it: a SoC that is deleted should not
  # take the record of what people downloaded with it. soc_model keeps the row
  # readable once the id means nothing.
  belongs_to :soc, optional: true

  # Only created, never updated, so there is no updated_at to maintain.
  self.record_timestamps = false

  # Recording must never cost somebody their download. A full disk, a locked
  # table, a migration not yet run on one container -- none of those are
  # reasons to fail a request that has already produced a valid image, so this
  # logs and carries on.
  def self.record(firmware:, soc:, bytes: nil)
    create!(soc: soc, soc_model: soc.model_downcase, flash_type: firmware.flash_type,
            release: firmware.release, flash_size: firmware.flash_size,
            bytes: bytes, created_at: Time.current)
  rescue StandardError => e
    # Loud, because a rescue this broad will otherwise hide a plain bug: this
    # returned nil for every call during development until the missing
    # belongs_to above was found, and the only sign was a nil where a row
    # should have been.
    Rails.logger.error "download not recorded for #{soc.model_downcase}: #{e.class}: #{e.message}"
    nil
  end
end
