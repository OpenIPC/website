# frozen_string_literal: true

class ProcessImagesJob < ApplicationJob
  queue_as :default

  def perform(snapshot)
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
    snapshot.update_column(:variants_generated_at, Time.current)
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
