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
      ids = root.children.select(&:directory?).map { |d| d.basename.to_s }
      live = Snapshot.where(id: ids).pluck(:id).map(&:to_s)
      orphans = ids - live
      orphans.each { |id| WallImage.purge(id) }
      puts "#{ids.size} directories, #{orphans.size} removed"
    else
      puts "#{root} does not exist"
    end
  end
end
