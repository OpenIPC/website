# frozen_string_literal: true

# Write the plain-file variants for one snapshot, reporting rather than raising
# so a single unreadable blob cannot stop a run of thousands. A row left
# unmarked simply stays on the ActiveStorage path, which is where it was.
def backfill_wall_images(snapshot)
  return :skipped unless snapshot.file.attached?

  ProcessImagesJob.perform_now(snapshot)
  :done
rescue StandardError => e
  warn "  snapshot #{snapshot.id}: #{e.class}: #{e.message}"
  :failed
end

# Move one snapshot's images from the directory named by its row id to the one
# named by its public_id. Returns what it did, so the task can count.
def rekey_wall_images(snapshot)
  from = WallImage.root.join(snapshot.id.to_s)
  return :absent unless from.directory?

  to = WallImage.dir_for(snapshot.public_id)
  return :present if to.exist?

  File.rename(from, to)
  :moved
end

namespace :wall do
  desc 'Write the plain-file variants for snapshots that predate them (#146)'
  task backfill: :environment do
    scope = Snapshot.where(variants_generated_at: nil).order(:id)
    puts "#{scope.count} snapshots without plain-file variants"

    counts = Hash.new(0)
    scope.find_each(batch_size: 100) do |snapshot|
      counts[backfill_wall_images(snapshot)] += 1
    end

    puts "backfilled #{counts[:done]}, skipped #{counts[:skipped]}, failed #{counts[:failed]}"
  end

  desc 'Remove wall directories with no snapshot row behind them (#146)'
  task prune: :environment do
    root = WallImage.root
    if root.directory?
      # Matched on public_id, which is what names these directories. It was
      # `where(id: keys)` while the directories were row ids, and left that
      # way it would have emptied the wall: MySQL casts a hex token to 0, no
      # row has id 0, so every live directory would have looked orphaned.
      keys = root.children.select(&:directory?).map { |d| d.basename.to_s }
      live = Snapshot.where(public_id: keys).pluck(:public_id)
      orphans = keys - live
      orphans.each { |key| WallImage.purge(key) }
      puts "#{keys.size} directories, #{orphans.size} removed"
    else
      puts "#{root} does not exist"
    end
  end

  # One-off, run once after the public_id migration deploys.
  #
  # The image files are the payload the crawl was after -- a page address
  # nobody can guess is worth nothing while /wall/<n>/fullhd.jpg still counts
  # from one -- so the directories are keyed on public_id now. Everything
  # already on disk is named by row id and would simply 404 behind the new
  # URLs until the two-day retention cleared it, which is both a broken wall
  # and two more days of a walkable one.
  #
  # Safe to re-run, and safe to run before the deploy finishes: it only ever
  # renames a directory named exactly like a row id belonging to a snapshot
  # that has one, and skips any destination that already exists.
  desc 'Rename wall directories from row id to public_id (#235)'
  task rekey: :environment do
    root = WallImage.root
    abort "#{root} does not exist" unless root.directory?

    numeric = root.children.select { |d| d.directory? && d.basename.to_s.match?(/\A[0-9]+\z/) }
    puts "#{numeric.size} directories still named by row id"

    counts = Hash.new(0)
    Snapshot.where(id: numeric.map { |d| d.basename.to_s }).find_each do |snapshot|
      counts[rekey_wall_images(snapshot)] += 1
    end

    puts "renamed #{counts[:moved]}, destination already present #{counts[:present]}, " \
         "left for wall:prune #{root.children.count { |d| d.directory? && d.basename.to_s.match?(/\A[0-9]+\z/) }}"
  end
end
