# frozen_string_literal: true

# The Open Wall's images as plain files on disk.
#
# Until 2026-09 every thumbnail on the wall was fetched through ActiveStorage's
# redirect mode: one request to /rails/active_storage/representations/redirect/
# answering a 302 to a /disk/ URL signed with a five minute expiry. Two Rails
# requests per image, both Cache-Control: private, the second one carrying a
# signature that changes -- so nothing between the camera owner and the disk
# could ever cache them.
#
# On 2026-09-18 that path was 293,785 requests, 62% of everything the site
# served, and 18,041 of the day's 21,291 429s. Almost none of it was people:
# 22,504 distinct addresses presenting a handful of spoofed mobile user agents,
# against roughly 1,600 real visitors a day.
#
# So the bytes are written once, where nginx can serve them without waking
# Ruby at all, under a URL that never changes for a given snapshot. A copy
# rather than a hardlink: the storage tree and this one are separate bind
# mounts in the container, and link(2) across mount points fails EXDEV even
# when the underlying filesystem is the same -- verified on the host before
# choosing. The cost is roughly a gigabyte at the two day retention, against
# forty free.
#
# It also leaves the wall independent of ActiveStorage, which #166 removes.
module WallImage
  VARIANTS = %i[icon icon2 thumb fullhd].freeze

  module_function

  # Under public/ so that Rails can still serve these if nginx is bypassed --
  # a backstop, not the intended path. Unlike public/files, which nginx 404s
  # because serving assembled firmware by name would skip the rules that decide
  # who may have it, these images are public by definition.
  #
  # A method rather than a constant, and per worker under test. The suite runs
  # parallelized across cores, each worker with its own database, so two
  # workers routinely mint the same snapshot id -- and a shared directory then
  # has one test deleting another's files mid-run. public/files has bitten this
  # way before. A constant cannot fix it: it is evaluated when the class loads,
  # which is before the workers fork and before TEST_ENV_NUMBER is set.
  def root
    return Rails.root.join('public', 'wall') unless Rails.env.test?

    Rails.root.join('tmp', "wall-test#{ENV.fetch('TEST_ENV_NUMBER', '')}")
  end

  def dir_for(snapshot_id)
    root.join(snapshot_id.to_s)
  end

  def path_for(snapshot_id, variant)
    dir_for(snapshot_id).join("#{variant}.jpg")
  end

  # The URL a page links to. Deliberately not a route: nothing in Rails serves
  # it in production, and naming it here keeps the shape in one place.
  def url_for(snapshot_id, variant)
    "/wall/#{snapshot_id}/#{variant}.jpg"
  end

  # Write one variant, atomically.
  #
  # Rename rather than write-in-place because nginx may be serving this path to
  # somebody while it is being replaced, and a half-written JPEG is worse than
  # an absent one. The temporary file is in the same directory so the rename
  # cannot cross a filesystem -- the same EXDEV trap that ruled out hardlinks,
  # and the one that once had Firmware#publish serving truncated images.
  def store(snapshot_id, variant, source_path)
    write_atomically(snapshot_id, variant) { |tmp| FileUtils.cp(source_path, tmp) }
  end

  # For a service that cannot say where its bytes are on disk.
  def store_bytes(snapshot_id, variant, data)
    write_atomically(snapshot_id, variant) { |tmp| File.binwrite(tmp, data) }
  end

  def write_atomically(snapshot_id, variant)
    dir = dir_for(snapshot_id)
    FileUtils.mkdir_p(dir)
    tmp = dir.join(".#{variant}.jpg.#{Process.pid}.#{SecureRandom.hex(4)}")
    yield tmp
    File.chmod(0o644, tmp)
    File.rename(tmp, path_for(snapshot_id, variant))
  ensure
    FileUtils.rm_f(tmp) if tmp && File.exist?(tmp)
  end

  def purge(snapshot_id)
    FileUtils.rm_rf(dir_for(snapshot_id))
  end
end
