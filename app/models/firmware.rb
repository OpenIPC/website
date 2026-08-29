# frozen_string_literal: true

require 'rubygems/package'

class Firmware
  # NAND full images stop after the UBI payload rather than spanning the chip.
  # The parts are 128MiB but the cameras have 64MB of RAM, so a chip-sized
  # image could never be staged at the load address to be written from.
  NAND_PAGE = 2048
  NAND_KERNEL_OFFSET = 0x100000
  NAND_ROOTFS_OFFSET = 0x400000

  NOR_SIZES = [8, 16, 32].freeze

  # The two mtdparts every OpenIPC bootloader carries. The layout is not the
  # chip: an 8MB layout on a 16MB part is a real configuration, and building it
  # as a chip-sized image is what lets the installation page erase the whole
  # part rather than stopping at the end of a smaller one.
  NOR_LAYOUTS = [8, 16].freeze

  LOCK_POLL = 0.1

  # Assembly of one image takes about a second. The wait exists only so that
  # concurrent requests for the same image do not each build it; anything near
  # this deadline means something is wedged, and failing is better than holding
  # one of Puma's sixteen threads indefinitely. Settable so the timeout path is
  # testable without a minute-long test, and tunable on a host without a deploy.
  class << self
    attr_accessor :lock_timeout

    # Where assembled images are cached. Settable so each test gets a
    # directory of its own: the suite parallelises across processes that share
    # one filesystem, and five tests generate the same NAND filename, so one
    # test's teardown was deleting a file another was still asserting on.
    # Resolved lazily -- Rails.root is not available while this class body runs
    # under eager loading.
    def cache_dir
      @cache_dir ||= Rails.root.join('public', 'files')
    end

    attr_writer :cache_dir
  end
  self.lock_timeout = 60

  # One part of the assembled image: what it is called in an error, its bytes,
  # where it is written, and the first offset it may not reach.
  Part = Struct.new(:name, :bytes, :offset, :limit, :limit_name)

  class MissingMember < StandardError; end
  class InvalidFlashSize < StandardError; end
  class PayloadTooLarge < StandardError; end
  class LockTimeout < StandardError; end

  def initialize(size: 8, flash_type: 'nor', release: 'lite', soc: nil, layout: nil)
    super()
    @soc = soc
    @size = size.to_i
    @flash_type = flash_type
    @release = release
    # Blank as well as nil, because this arrives from a query string and an
    # older permanent link carries no layout at all.
    @layout = layout.to_s.strip.empty? ? self.class.natural_layout(@size) : layout.to_i
  end

  # The layout a chip of this size wears unless told otherwise, and the one
  # every image built before there was a choice was built with.
  def self.natural_layout(size)
    size.to_i <= 8 ? 8 : 16
  end

  # Built here rather than in the view so the TFTP command on the installation
  # page and the file the download actually produces cannot drift apart.
  #
  # The layout only enters the name when it is not the chip's own. Every image
  # already on disk was built before the two could differ, so leaving the usual
  # combinations spelled as they always were keeps that cache valid -- and keeps
  # the file a visitor downloads named after the chip they told us about.
  def self.filename_for(soc_model:, flash_type:, release:, size:, layout: nil)
    return "openipc-#{soc_model}-nand-#{release}.bin" if flash_type.to_s.eql?('nand')

    layout = natural_layout(size) if layout.to_s.strip.empty?
    suffix = layout.to_i.eql?(natural_layout(size)) ? '' : "-parts#{layout.to_i}m"
    "openipc-#{soc_model}-nor-#{release}-#{size}mb#{suffix}.bin"
  end

  def filename
    @filename ||= self.class.filename_for(soc_model: @soc.model_downcase, flash_type: @flash_type,
                                          release: @release, size: @size, layout: @layout)
  end

  def filepath
    @filepath ||= File.join(self.class.cache_dir, filename)
  end

  def nand?
    @flash_type.to_s.eql?('nand')
  end

  # What this was asked to build, for the record kept of it. Read rather than
  # re-derived by the caller, so the two cannot disagree about which image was
  # actually sent.
  attr_reader :flash_type, :release

  def flash_size
    @size
  end

  # Build if it needs building, and serve what is already there if the parts
  # cannot be reached.
  #
  # The order used to be fixed: resolve both sources, then ask whether the image
  # was fresh. Resolving goes through ReleaseIndex, so losing .index.json
  # refused images that were already built, complete and current on disk --
  # verified, not theorised: an 8,388,608-byte ssc338q image was refused with
  # `no release index`. The index is one 44KB file written by an hourly cron,
  # and every firmware download on the site depended on it being readable.
  #
  # Unavailable means the parts cannot be had *right now* -- no index, GitHub
  # unreachable, bytes that did not match. If a usable image is already on disk,
  # that is the whole answer to the request, so give it. UnknownAsset is not
  # caught: it means upstream does not publish this at all, and a leftover image
  # for a name that has been aliased away is exactly what should not be served.
  def generate
    validate_size!
    validate_layout!
    build_if_needed
  rescue ReleaseCache::Unavailable => e
    raise unless usable?

    Rails.logger.warn "firmware: serving the cached #{filename}, its parts are unavailable: #{e.message}"
    nil
  end

  private

  def build_if_needed
    uboot_file = @soc.uboot_file
    unless File.exist?(uboot_file)
      Rails.logger.warn "firmware: #{uboot_file} not found"
      return
    end

    linux_file = @soc.linux_file(@release, @flash_type)
    unless File.exist?(linux_file)
      Rails.logger.warn "firmware: #{linux_file} not found"
      return
    end

    return if fresh?(uboot_file, linux_file)

    FileUtils.mkdir_p File.dirname(filepath)
    with_lock do
      # Whoever held the lock may have been building this very image, so ask
      # again now rather than assembling a second identical copy.
      return if fresh?(uboot_file, linux_file)

      assemble(uboot_file, linux_file)
    end
  end

  # Present, servable, and the size it claims to be. Everything fresh? asks
  # except whether it is newer than its parts -- which is the one question that
  # needs the parts, and therefore the index.
  #
  # Three separate stats, so the file can go between them: deploy/purge-firmware
  # -cache.sh deletes images older than fourteen days and nothing coordinates
  # with it. An ENOENT escaping here would turn a handled "try again shortly"
  # into a 500, and on the fallback path it would replace an accurate message
  # with a crash. Gone is simply not usable.
  def usable?
    File.exist?(filepath) && web_readable? && right_size?
  rescue SystemCallError => e
    Rails.logger.warn "firmware: cannot examine the cached #{filename}: #{e.class}: #{e.message}"
    false
  end

  # file exists, can be served, is the size it claims to be, and is newer than
  # any of its parts
  def fresh?(uboot_file, linux_file)
    usable? &&
      File.mtime(uboot_file) < File.mtime(filepath) &&
      File.mtime(linux_file) < File.mtime(filepath)
  end

  # nginx serves this directory and runs as neither the owner nor the group, so
  # an image it cannot read is not usable even though it is present. Counting
  # that as stale rebuilds the images published at 0600 before the mode was
  # fixed, instead of needing a chmod by hand -- and closes the gap between the
  # rename and the chmod above, where the file exists but is not yet servable.
  def web_readable?
    return true if (File.stat(filepath).mode & 0o004).positive?

    Rails.logger.warn "firmware: the web server cannot read the cached #{filename}"
    false
  end

  # A NOR image is exactly its flash size by construction, so anything else is
  # a leftover from before that was enforced: the 9,465,856-byte
  # openipc-hi3516ev200-nor-ultimate-8mb.bin, or a partial image from the
  # cross-filesystem copy this class used to publish with. public/files is a
  # host mount that outlives deploys and nothing prunes it, so checking only
  # mtimes would go on serving those until their source tarball happened to
  # change. NAND is sized from its payload and has no equivalent invariant.
  def right_size?
    return true if nand?

    actual = File.size(filepath)
    return true if actual == @size.megabytes

    Rails.logger.warn "firmware: cached #{filename} is #{actual} bytes, expected #{@size.megabytes}"
    false
  end

  # The build itself. Everything here happens on a temporary file; `filepath`
  # is only ever created by the rename at the end.
  def assemble(uboot_file, linux_file)
    data, present = read_members(linux_file, [kernel_member, rootfs_member])
    kernel = fetch_member!(data, kernel_member, linux_file, present)
    rootfs = fetch_member!(data, rootfs_member, linux_file, present)

    # Resolve every offset and the length before allocating anything, so an
    # unsupported size cannot get as far as filling a buffer with it.
    size = image_size(rootfs)
    parts = layout(IO.binread(uboot_file), kernel, rootfs, size)
    validate_fit!(parts)

    publish(size, parts)
  end

  # Where each part goes, and the first offset it may not reach. Written in
  # this order so a part can only ever be overwritten by one that follows it.
  def layout(uboot, kernel, rootfs, size)
    [
      Part.new('u-boot', uboot, 0, kernel_offset, 'the kernel offset'),
      Part.new('kernel', kernel, kernel_offset, rootfs_offset, 'the rootfs offset'),
      Part.new('rootfs', rootfs, rootfs_offset, *rootfs_limit(size))
    ]
  end

  # The rootfs may not run past its own partition, which on NOR is where the
  # overlay starts and not the end of the image.
  #
  # The two were near enough the same number while the layout was the chip: an
  # 8MB image laid out the 8MB way ends 0xb0000 past the rootfs partition, and
  # the difference only mattered for a rootfs already too big for the partition
  # to mount. A 16MB image laid out the 8MB way ends 0x8b0000 past it, and
  # download_full_image takes the edition and the layout straight from the
  # query string -- so `?fw_release=ultimate&flash_size=16&layout=8` would have
  # written a 7MB rootfs from 0x250000 clean through rootfs_data, and the
  # camera would have mounted a squashfs whose tail the overlay then formatted
  # over.
  #
  # NAND keeps the end of the image, which is where its rootfs ends by
  # construction: image_size is the rootfs offset plus the payload.
  def rootfs_limit(size)
    return [size, 'the end of the image'] if nand?

    [nor_layout[:overlay_offset], 'the rootfs partition']
  end

  # Build beside the destination and rename into place.
  #
  # This used to build in Dir.tmpdir and hand the result to FileUtils.mv. In
  # the container /tmp is the overlay and public/files is a bind mount from the
  # host, so File.rename raised EXDEV and mv fell back to copy-then-unlink --
  # writing 8-32MB straight into the live filename. Since `fresh?` is satisfied
  # by a file that merely exists and has a recent mtime, a request arriving
  # during that copy was served a partial image, and an interrupted copy left
  # one on disk to be served indefinitely. Nothing publishes a checksum for the
  # assembled .bin, so neither we nor the user could detect it.
  #
  # A rename within one directory is atomic, so filepath only ever names a
  # complete image.
  def publish(size, parts)
    purge_stale_builds
    tmp = Tempfile.create([tmp_prefix, '.bin'], File.dirname(filepath))
    tmp.close
    IO.binwrite tmp.path, ("\xFF" * size)
    parts.each { |part| IO.binwrite tmp.path, part.bytes, part.offset }

    # Tempfile creates at 0600 and rename keeps the mode, so every image ever
    # published here was readable only by the app's own user. nginx runs as
    # www-data and serves this directory, so /files/ answered 403 for all of
    # them -- and nothing could hand a download off to nginx while that was
    # true.
    #
    # Widened after the rename rather than before it. The temporary file lives
    # in the same served directory, and there is no reason for a half-built
    # image to be readable by anything, however unguessable its name and
    # however short its life. `fresh?` refuses an image the web server cannot
    # read, so the moment between the two is not one another request can be
    # served out of.
    File.rename(tmp.path, filepath)
    File.chmod(0o644, filepath)
  ensure
    # `ensure` rather than `rescue StandardError`, so an Interrupt or a
    # SignalException clears up after itself too. After the rename the path is
    # gone, so this is a no-op on the happy path.
    FileUtils.rm_f(tmp.path) if tmp && File.exist?(tmp.path)
  end

  # Debris used to land in Dir.tmpdir, which the container discards on restart.
  # It now lands beside the image, in a directory nginx serves and nothing
  # prunes, so a build killed outright -- SIGKILL, OOM, a container stopped
  # mid-write -- would leave a partial image there for good.
  #
  # Running under the lock is what makes this safe: no other process is
  # building this image, so any temporary file bearing its prefix is from a
  # build that is already over. Each image clears its own debris on its next
  # build, which bounds the leftovers at one per image never built again.
  def purge_stale_builds
    stale = Dir.glob(File.join(File.dirname(filepath), "#{tmp_prefix}*"))
    return if stale.empty?

    Rails.logger.warn "firmware: clearing #{stale.size} abandoned build(s) of #{filename}"
    FileUtils.rm_f(stale)
  end

  # size is a request parameter, so it has to be checked before it can be turned
  # into an allocation. Guarding here rather than relying on rootfs_offset to
  # raise later keeps a crafted ?flash_size=<huge> from filling a buffer first.
  # NAND ignores it entirely: that image is sized from its payload.
  def validate_size!
    return if nand?
    return if NOR_SIZES.include?(@size)

    raise InvalidFlashSize, "unsupported NOR flash size #{@size}MB (expected #{NOR_SIZES.join(', ')})"
  end

  # Same reasoning as validate_size!, for the same reason: the layout is a
  # request parameter too, and it decides the offsets everything is written at.
  # A layout larger than the chip is refused rather than clamped -- the 16MB one
  # puts the rootfs at 0x350000 with 10240KB to fill, which does not exist on an
  # 8MB part, and silently substituting the other one would hand back an image
  # that does not match the name it was asked for.
  def validate_layout!
    return if nand?
    return if NOR_LAYOUTS.include?(@layout) && @layout <= @size

    raise InvalidFlashSize, "unsupported NOR partition layout #{@layout}MB on a #{@size}MB chip"
  end

  # IO.binwrite past the end of a file grows it rather than failing, so a part
  # too big for its slot produced an image larger than the chip it names
  # instead of an error. openipc-hi3516ev200-nor-ultimate-8mb.bin was found on
  # disk at 9,465,856 bytes: 0x250000 for the rootfs offset plus a 7MB Ultimate
  # rootfs, in a file whose name promises 8MB. The installation wizard refuses
  # Ultimate on 8MB flash, but download_full_image takes flash_size and
  # fw_release straight from the query string and had no equivalent check.
  #
  # Checked against the parts themselves rather than by re-deriving the rule,
  # so this also covers a build that simply outgrows its partition.
  def validate_fit!(parts)
    parts.each do |part|
      bytes = part.bytes.bytesize
      overrun = part.offset + bytes - part.limit
      next if overrun <= 0

      raise PayloadTooLarge,
            "#{part.name} is #{bytes} bytes at 0x#{part.offset.to_s(16)}, which runs #{overrun} bytes " \
            "past #{part.limit_name} (0x#{part.limit.to_s(16)}) of #{filename}"
    end
  end

  # Serialises assembly of one image across requests and processes. The lock
  # file is separate from the image so the rename cannot invalidate it, and
  # acquisition is a non-blocking poll against a deadline because flock offers
  # no timeout and a blocked thread here is a thread the whole site loses.
  def with_lock
    lock = File.open(lock_path, File::CREAT | File::RDWR, 0o644)
    timeout = self.class.lock_timeout
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + timeout
    until lock.flock(File::LOCK_EX | File::LOCK_NB)
      raise LockTimeout, "waited #{timeout}s for another request to finish building #{filename}" \
        if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline

      sleep LOCK_POLL
    end
    yield
  ensure
    lock&.flock(File::LOCK_UN)
    lock&.close
  end

  def lock_path
    File.join(File.dirname(filepath), ".#{filename}.lock")
  end

  # Dot-prefixed so nginx's autoindex on /files/ does not list a half-built
  # image, and named for the target so a leftover says which build died.
  def tmp_prefix
    ".tmp-#{filename}-"
  end

  # One pass, reading only the two bodies we need -- rootfs.ubi runs to 16MB and
  # buffering every member would hold far more than the image itself. Names are
  # collected for the error message, which costs nothing: skipped entries are
  # stepped over by header, not read.
  #
  # A pass rather than two TarReader#seek calls because seek scans forward from
  # the current position, so successive seeks only worked while the members
  # happened to be in the order asked for.
  def read_members(linux_file, wanted)
    data = {}
    present = []
    Gem::Package::TarReader.new(Zlib::GzipReader.open(linux_file)) do |tar|
      tar.each do |entry|
        next unless entry.file?

        present << entry.full_name
        data[entry.full_name] = entry.read if wanted.include?(entry.full_name)
      end
    end
    [data, present]
  end

  # The bug this class shipped for years: TarReader#seek yields nothing when the
  # member is absent and the return value was never checked, so a NAND build --
  # whose rootfs is named rootfs.ubi, not rootfs.squashfs -- produced an image
  # with no root filesystem in it at all, served as though it were complete.
  # Refusing loudly is also what keeps SigmaStar and Rockchip out: their NAND
  # tarballs carry no kernel member, because the kernel is a volume inside the
  # UBI image rather than a partition of its own.
  def fetch_member!(data, name, linux_file, present)
    body = data[name]
    return body if body

    listed = present.reject { |k| k.end_with?('.md5sum') }.join(', ')
    raise MissingMember, "#{name} is not in #{File.basename(linux_file)} (members: #{listed})"
  end

  # Members are named for the board the firmware was built for, which is not
  # always the SoC model -- see Soc#board.
  def board
    @board ||= @soc.board
  end

  def kernel_member
    "uImage.#{board}"
  end

  def rootfs_member
    return "rootfs.ubi.#{board}" if nand?

    "rootfs.squashfs.#{board}"
  end

  # Same table the installation page renders from, so the image and the
  # instructions cannot describe different partition layouts. Keyed on the
  # layout rather than the size, which are the same thing for every image built
  # before the wizard could tell them apart, and on the vendor, because
  # SigmaStar and Ingenic have one mtdparts string whose rootfs offset does not
  # move -- an image that put it where the other table says is one their
  # bootloader cannot boot.
  def nor_layout
    @nor_layout ||= FlashLayout.nor(@layout, @soc.vendor.name)
  end

  def kernel_offset
    return NAND_KERNEL_OFFSET if nand?

    nor_layout[:kernel_offset]
  end

  def rootfs_offset
    return NAND_ROOTFS_OFFSET if nand?

    nor_layout[:rootfs_offset]
  end

  def image_size(rootfs)
    return @size.megabytes unless nand?

    end_of_rootfs = NAND_ROOTFS_OFFSET + rootfs.bytesize
    # Page-align so the generated image can be written with ${filesize}; U-Boot
    # `nand write` rejects a length that is not a multiple of the page size.
    (end_of_rootfs + NAND_PAGE - 1) / NAND_PAGE * NAND_PAGE
  end
end
