# frozen_string_literal: true

# Retires Open Wall snapshots past their retention window.
#
# Run from cron once a night, not from the request path. It used to be enqueued
# on every upload, so with ~1,400 cameras it ran thousands of times a day, each
# doing a full table scan.
class PurgeImagesJob < ApplicationJob
  queue_as :default

  RETENTION = 2.days
  BATCH_SIZE = 200

  def perform(retention: RETENTION)
    purged = 0

    Snapshot.where(created_at: ...retention.ago)
            .in_batches(of: BATCH_SIZE) do |batch|
      batch.each do |snapshot|
        # purge, not purge_later, and destroy, not delete. The queue adapter is
        # :async and has no persistence, so a deferred purge is simply lost on
        # the next restart -- and by then the attachment row is already gone,
        # leaving the blob unreachable forever.
        snapshot.file.purge if snapshot.file.attached?
        snapshot.destroy
        purged += 1
      rescue ActiveStorage::FileNotFoundError
        # The row outlived its file. Still drop the row.
        snapshot.destroy
        purged += 1
      end
    end

    Rails.logger.info("[PurgeImagesJob] purged #{purged} snapshots older than #{retention.inspect}")
    purged
  end
end
