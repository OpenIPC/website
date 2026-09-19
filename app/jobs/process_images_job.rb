# frozen_string_literal: true

# Turns an upload into the four images the Open Wall serves.
#
# Two jobs in one, and the order between them is the whole contract: generate
# the ActiveStorage variants, then write them where nginx can serve them
# directly, and only then mark the row so the views start linking to the files.
# See WallImage for why they are copied rather than linked, and #146 for the
# 293,785 requests a day this exists to stop.
class ProcessImagesJob < ApplicationJob
  queue_as :default

  def perform(snapshot)
    # Nothing to do for a row that has gone. The :async adapter usually spares
    # us this -- GlobalID cannot find the record and ActiveJob discards the job
    # before perform -- but the backfill calls perform_now directly.
    return unless Snapshot.exists?(snapshot.id)

    # Pre-process the named variants the wall actually renders. This also forces
    # HEIF uploads to be decoded once here, in the background, instead of on the
    # first page view.
    WallImage::VARIANTS.each do |name|
      variant = snapshot.file.variant(name).processed
      store(snapshot, name, variant)
    end

    # Last, and only if every variant landed: the column is what the views read
    # to decide whether they may link to the plain files, so it must never be
    # set while one of them is missing. A failure part way through leaves it
    # null and the page falls back to ActiveStorage, which is where it was.
    #
    # And only if the row is still there. Destruction purges the directory, so
    # a destroy that lands mid-run has this job recreate it behind the purge
    # and then update nothing -- update_column on a deleted row matches no
    # rows and raises nothing -- leaving files no snapshot references. Rare:
    # the nightly purge only touches rows two days old, whose jobs ran two days
    # ago, so the window is really the backfill overlapping that purge. Cheap
    # to close anyway, and wall:prune remains the backstop for the interleaving
    # this still cannot see.
    if Snapshot.exists?(snapshot.id)
      snapshot.update_column(:variants_generated_at, Time.current)
    else
      WallImage.purge(snapshot.id)
    end
  end

  private

  # Copy the bytes the variant already wrote rather than pulling them through
  # Ruby. #download would build a String from a 1920x1080 JPEG for no reason;
  # the Disk service can say where it is. Any other service still has to.
  def store(snapshot, name, variant)
    blob = variant.image.blob
    service = ActiveStorage::Blob.service

    if service.respond_to?(:path_for)
      WallImage.store(snapshot.id, name, service.path_for(blob.key))
    else
      WallImage.store_bytes(snapshot.id, name, blob.download)
    end
  end
end
