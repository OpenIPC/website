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
  # A method rather than a constant, and per process under test. The suite runs
  # parallelized across cores, each worker with its own database, so two
  # workers routinely mint the same snapshot id -- and a shared directory then
  # has one test deleting another's files mid-run. public/files has bitten this
  # way before. A constant cannot fix it: it is evaluated when the class loads,
  # which is before the workers fork.
  #
  # Keyed on the pid, because TEST_ENV_NUMBER is not set here. That is a
  # parallel_tests convention; `parallelize(workers: :number_of_processors)`
  # gives each worker its own database but does not export it, so every one of
  # the 32 processes read it as "" and shared one directory. Measured: three
  # pids, one root between them. The isolation this comment described was not
  # happening, and the symptom was an occasional ENOENT between an
  # assert_path_exists and the File.binread on the next line.
  def root
    return Rails.root.join('public', 'wall') unless Rails.env.test?

    Rails.root.join('tmp', "wall-test-#{Process.pid}")
  end

  # Keyed on the snapshot's public_id, not its row id. The images are the
  # payload: a page address nobody can guess is worth nothing while
  # /wall/<n>/fullhd.jpg still counts from one.
  def dir_for(key)
    root.join(key.to_s)
  end

  def path_for(key, variant)
    dir_for(key).join("#{variant}.jpg")
  end

  # The URL a page links to. Deliberately not a route: nothing in Rails serves
  # it in production, and naming it here keeps the shape in one place.
  def url_for(key, variant)
    "/wall/#{key}/#{variant}.jpg"
  end

  # Write one variant, atomically.
  #
  # Rename rather than write-in-place because nginx may be serving this path to
  # somebody while it is being replaced, and a half-written JPEG is worse than
  # an absent one. The temporary file is in the same directory so the rename
  # cannot cross a filesystem -- the same EXDEV trap that ruled out hardlinks,
  # and the one that once had Firmware#publish serving truncated images.
  def store(key, variant, source_path)
    write_atomically(key, variant) { |tmp| FileUtils.cp(source_path, tmp) }
  end

  # For a service that cannot say where its bytes are on disk.
  def store_bytes(key, variant, data)
    write_atomically(key, variant) { |tmp| File.binwrite(tmp, data) }
  end

  def write_atomically(key, variant)
    dir = dir_for(key)
    FileUtils.mkdir_p(dir)
    tmp = dir.join(".#{variant}.jpg.#{Process.pid}.#{SecureRandom.hex(4)}")
    yield tmp
    File.chmod(0o644, tmp)
    File.rename(tmp, path_for(key, variant))
  ensure
    FileUtils.rm_f(tmp) if tmp && File.exist?(tmp)
  end

  def purge(key)
    FileUtils.rm_rf(dir_for(key))
  end
end
