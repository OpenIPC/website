# frozen_string_literal: true

# Reclaims ActiveStorage blobs that nothing references any more.
#
#   rails storage:orphans                 report only, changes nothing
#   rails storage:reap                    delete them (asks for the count first)
#   rails storage:reap LIMIT=5000         work through it in chunks
#
# Why these exist: Admin::SnapshotsController used delete/delete_all, which skip
# the has_one_attached destroy callback, so the Snapshot row went away while its
# blob, its variant blobs and their files stayed behind with nothing pointing at
# them. PurgeImagesJob compounded it by deleting the attachment row synchronously
# while deferring the blob purge to the :async adapter, which drops pending jobs
# on every restart. Both are fixed; this clears what they left.
#
# Everything here purges SYNCHRONOUSLY. Using purge_later would hand the work
# back to the same queue adapter that caused the problem.

namespace :storage do
  BATCH = 500

  # An attachment whose owning record no longer exists.
  def dangling_attachments
    ActiveStorage::Attachment
      .where(record_type: 'Snapshot')
      .where.not(record_id: Snapshot.select(:id))
  end

  # A variant record whose parent blob is no longer attached to a live Snapshot.
  def dead_variant_records
    live_blob_ids = ActiveStorage::Attachment
                    .where(record_type: 'Snapshot', record_id: Snapshot.select(:id))
                    .select(:blob_id)
    ActiveStorage::VariantRecord.where.not(blob_id: live_blob_ids)
  end

  # A blob with no attachment row at all.
  #
  # Skip anything created recently. A blob is written before its attachment row
  # commits, so an upload in flight looks exactly like an orphan for a moment.
  # An hour is far longer than that window and costs nothing -- these have been
  # accumulating since 2023.
  GRACE = 1.hour

  def unattached_blobs
    ActiveStorage::Blob
      .where.missing(:attachments)
      .where(created_at: ...GRACE.ago)
  end

  def report
    counts = {
      'snapshots (live)' => Snapshot.count,
      'blobs' => ActiveStorage::Blob.count,
      'attachments' => ActiveStorage::Attachment.count,
      'variant records' => ActiveStorage::VariantRecord.count,
      '--' => nil,
      'dangling attachments (owner deleted)' => dangling_attachments.count,
      'variant records with dead parent' => dead_variant_records.count,
      'blobs with no attachment' => unattached_blobs.count
    }
    width = counts.keys.map(&:length).max
    counts.each do |label, n|
      next puts('  ' + '-' * (width + 12)) if n.nil?

      puts format("  %-#{width}s  %8d", label, n)
    end
    bytes = unattached_blobs.sum(:byte_size)
    puts format("  %-#{width}s  %8s", 'reclaimable (unattached only)', "#{(bytes / 1024.0 / 1024).round(1)} MB")
  end

  desc 'Report ActiveStorage orphans without changing anything'
  task orphans: :environment do
    report
  end

  desc 'Purge ActiveStorage orphans (LIMIT=n to cap the run)'
  task reap: :environment do
    limit = ENV['LIMIT']&.to_i
    done = 0
    failed = 0

    stop = lambda do
      next false unless limit

      done >= limit
    end

    # Order matters. Clearing dangling attachments first turns their blobs into
    # unattached blobs, which the third pass then collects -- so a blob is never
    # deleted while something still points at it.
    puts '==> pass 1: attachments whose Snapshot is gone'
    loop do
      batch = dangling_attachments.limit(BATCH).to_a
      break if batch.empty? || stop.call

      batch.each do |att|
        att.purge
        done += 1
      rescue StandardError => e
        # A missing file is expected on a tree this old; drop the row anyway.
        warn "    #{e.class} on attachment #{att.id}: #{e.message}"
        att.destroy
        failed += 1
      end
      print "\r    #{done} purged"
    end
    puts

    puts '==> pass 2: variant records whose parent is gone'
    loop do
      batch = dead_variant_records.limit(BATCH).to_a
      break if batch.empty? || stop.call

      batch.each do |vr|
        vr.image.purge if vr.image.attached?
        vr.destroy
        done += 1
      rescue StandardError => e
        warn "    #{e.class} on variant_record #{vr.id}: #{e.message}"
        vr.destroy
        failed += 1
      end
      print "\r    #{done} purged"
    end
    puts

    puts '==> pass 3: blobs with nothing attached'
    loop do
      batch = unattached_blobs.limit(BATCH).to_a
      break if batch.empty? || stop.call

      batch.each do |blob|
        blob.purge
        done += 1
      rescue StandardError => e
        warn "    #{e.class} on blob #{blob.id}: #{e.message}"
        blob.destroy
        failed += 1
      end
      print "\r    #{done} purged"
    end
    puts

    puts
    puts "reaped #{done} records (#{failed} needed a fallback destroy)"
    puts
    report
  end
end
