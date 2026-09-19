# frozen_string_literal: true

# Records that a snapshot's four wall variants have been written to disk as
# plain files, so a page can link straight to them instead of asking
# ActiveStorage for a signed, expiring redirect. See issue #146.
#
# A column rather than a stat() per image: the Open Wall renders eighteen
# thumbnails a page and the homepage five, and the question "has the job run
# yet" has to be answerable without touching the filesystem or the
# variant_records table on every render.
#
# Nullable, and null means "not yet" -- which is what every existing row is
# until the backfill runs, and what a row is between its upload and its job.
# Both cases fall back to the ActiveStorage URL, so this is safe to deploy
# before anything has been generated.
class AddVariantsGeneratedAtToSnapshots < ActiveRecord::Migration[8.1]
  def change
    add_column :snapshots, :variants_generated_at, :datetime
  end
end
