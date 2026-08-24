# frozen_string_literal: true

require 'test_helper'
require 'rubygems/package'
require 'zlib'
require 'tmpdir'
require 'benchmark'
require 'minitest/mock'

class FirmwareTest < ActiveSupport::TestCase
  StubVendor = Struct.new(:name)

  class StubSoc
    attr_reader :model_downcase, :vendor, :uboot_file, :board

    # board defaults to the model because for most SoCs they are the same. The
    # ones where they are not -- T23N on the t23 build, the GK7102 variants,
    # AK3916EV301 on the AK3918EV200 build -- are what Soc#board exists for.
    def initialize(model:, vendor:, uboot_file:, linux_file:, board: nil)
      @model_downcase = model
      @board = board || model
      @vendor = StubVendor.new(vendor)
      @uboot_file = uboot_file
      @linux_file = linux_file
    end

    def linux_file(_release, _flash_type)
      @linux_file
    end
  end

  UBOOT = "\x17\x04\x00\xEA".b + ("\x5A".b * 0x2000)
  KERNEL = "\x27\x05\x19\x56".b + ("\xA5".b * 0x1000)
  SQUASHFS = 'hsqs'.b + ("\xC3".b * 0x2000)
  UBI = 'UBI#'.b + ("\xD7".b * 0x3000)

  def setup
    @dir = Dir.mktmpdir
    # Each test caches into a directory of its own. The suite parallelises
    # across processes that share one filesystem, and several tests generate
    # the same filename -- five of them produce
    # openipc-hi3516ev300-nand-ultimate.bin -- so without this one test's
    # teardown deletes a file another is still reading.
    @cache = Dir.mktmpdir
    Firmware.cache_dir = @cache
  end

  def teardown
    Firmware.cache_dir = nil
    FileUtils.remove_entry(@dir)
    FileUtils.remove_entry(@cache)
  end

  def write_tgz(name, members)
    path = File.join(@dir, name)
    Zlib::GzipWriter.open(path) do |gz|
      Gem::Package::TarWriter.new(gz) do |tar|
        members.each do |member, data|
          tar.add_file_simple(member, 0o644, data.bytesize) { |io| io.write(data) }
        end
      end
    end
    path
  end

  # Camera only asks a SoC for its vendor name when choosing a layout.
  def fw_soc(vendor)
    StubSoc.new(model: 'x', vendor: vendor, uboot_file: '', linux_file: '')
  end

  def uboot_path
    @uboot_path ||= File.join(@dir, 'u-boot.bin').tap { |p| IO.binwrite(p, UBOOT) }
  end

  def build(model:, vendor:, members:, flash_type:, size:, release: 'ultimate', board: nil)
    soc = StubSoc.new(model: model, vendor: vendor, board: board, uboot_file: uboot_path,
                      linux_file: write_tgz("openipc.#{board || model}-#{flash_type}-#{release}.tgz", members))
    Firmware.new(size: size, flash_type: flash_type, release: release, soc: soc)
  end

  # Half-built images are named for their target, so a leftover is attributable
  # to one build rather than to the suite as a whole.
  def leftover_temp_files(firmware)
    Dir.glob(File.join(File.dirname(firmware.filepath), ".tmp-#{firmware.filename}-*"))
  end

  # --- filename ---

  test 'filename carries the flash type so nor and nand cannot share a cache file' do
    nor = Firmware.filename_for(soc_model: 'hi3516ev300', flash_type: 'nor', release: 'ultimate', size: 16)
    nand = Firmware.filename_for(soc_model: 'hi3516ev300', flash_type: 'nand', release: 'ultimate', size: 16)
    assert_equal 'openipc-hi3516ev300-nor-ultimate-16mb.bin', nor
    assert_equal 'openipc-hi3516ev300-nand-ultimate.bin', nand
    assert_not_equal nor, nand
  end

  # --- NOR regression ---

  test 'nor image keeps its layout' do
    fw = build(model: 'hi3518ev200', vendor: 'HiSilicon', flash_type: 'nor', size: 16,
               members: { 'uImage.hi3518ev200' => KERNEL, 'rootfs.squashfs.hi3518ev200' => SQUASHFS })
    fw.generate
    image = IO.binread(fw.filepath)

    assert_equal 16.megabytes, image.bytesize
    assert_equal UBOOT, image[0, UBOOT.bytesize]
    assert_equal KERNEL, image[0x50000, KERNEL.bytesize]
    assert_equal SQUASHFS, image[0x350000, SQUASHFS.bytesize]
  end

  # --- NAND ---

  test 'nand image takes rootfs.ubi and puts it at the ubi offset' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })
    fw.generate
    image = IO.binread(fw.filepath)

    assert_equal UBOOT, image[0, UBOOT.bytesize]
    assert_equal KERNEL, image[0x100000, KERNEL.bytesize], 'kernel belongs at the NAND offset'
    assert_equal UBI, image[0x400000, UBI.bytesize], 'rootfs.ubi belongs at the UBI offset'
    assert_equal 'UBI#', image[0x400000, 4]
  end

  test 'nand image stops after the payload and is page aligned' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })
    fw.generate
    size = File.size(fw.filepath)

    assert_equal 0, size % 2048, 'nand write rejects a non-page-aligned length'
    assert size >= 0x400000 + UBI.bytesize
    assert size < 0x8000000, 'a chip-sized image could not be staged in 64MB of RAM'
  end

  # --- the silent hole this PR closes ---

  test 'a nand tarball with no kernel is refused rather than served' do
    # SigmaStar and Rockchip carry the kernel as a volume inside rootfs.ubi, so
    # their NAND tarball has no uImage member and no full image can be built.
    fw = build(model: 'ssc338q', vendor: 'SigmaStar', flash_type: 'nand', size: 128,
               members: { 'rootfs.ubi.ssc338q' => UBI })

    error = assert_raises(Firmware::MissingMember) { fw.generate }
    assert_match(/uImage\.ssc338q/, error.message)
    assert_not File.exist?(fw.filepath), 'nothing may be left behind for send_file to serve'
  end

  test 'a nor tarball without a squashfs is refused rather than silently omitted' do
    # The original bug: NAND tarballs ship rootfs.ubi, the generator asked for
    # rootfs.squashfs, TarReader#seek returned nil, and the rootfs was dropped.
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nor', size: 16,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })

    error = assert_raises(Firmware::MissingMember) { fw.generate }
    assert_match(/rootfs\.squashfs\.hi3516ev300/, error.message)
  end

  # --- flash_size arrives straight from the query string ---

  test 'an unsupported nor size is refused before anything is allocated' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nor', size: 999_999,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.squashfs.hi3516ev300' => SQUASHFS })

    # Must raise on the size itself, not after filling 999999MB of buffer.
    elapsed = Benchmark.realtime { assert_raises(Firmware::InvalidFlashSize) { fw.generate } }
    assert elapsed < 5, "took #{elapsed}s -- the size was allocated before it was checked"
    assert_not File.exist?(fw.filepath)
  end

  test 'every supported nor size is accepted' do
    [8, 16, 32].each do |size|
      fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nor', size: size,
                 members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.squashfs.hi3516ev300' => SQUASHFS })
      fw.generate
      assert_equal size.megabytes, File.size(fw.filepath), "size #{size} did not generate"
    end
  end

  test 'nand ignores the size parameter entirely' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 999_999,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI })
    fw.generate

    # Sized from the payload, then rounded up to a whole page: U-Boot's
    # `nand write` rejects a length that is not a multiple of the page size,
    # and the generated image is written with ${filesize}.
    expected = ((0x400000 + UBI.bytesize) + 2047) / 2048 * 2048
    assert_equal expected, File.size(fw.filepath),
                 'a NAND image is sized from its payload, never from the parameter'
    assert_equal 0, File.size(fw.filepath) % 2048, 'image length must be page aligned'
  end

  test 'only the members needed are read into memory' do
    # A distinctive sentinel, not a run of NULs: a firmware blob can legitimately
    # contain long zero runs, so searching for those would fail on a clean image.
    sentinel = 'UNWANTED-MEMBER-DO-NOT-COPY'.b
    big = sentinel * (4 * 1024 * 1024 / sentinel.bytesize)
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'uImage.hi3516ev300' => KERNEL, 'rootfs.ubi.hi3516ev300' => UBI,
                          'unrelated.blob' => big })
    fw.generate

    # The unrelated member must not appear anywhere in the assembled image.
    assert_equal UBI, IO.binread(fw.filepath)[0x400000, UBI.bytesize]

    # Size is the tell: it is fixed by the rootfs offset plus the UBI payload,
    # so anything else finding its way in would move it. Comparing against
    # big.bytesize cannot work -- the rootfs offset alone is 4 MiB, so a NAND
    # image is always at least that large.
    expected = ((0x400000 + UBI.bytesize) + 2047) / 2048 * 2048
    assert_equal expected, File.size(fw.filepath), 'an unwanted member leaked into the image'
    refute_includes IO.binread(fw.filepath), sentinel, 'unwanted member content is present'
  end

  test 'the missing-member error still names what the tarball does hold' do
    fw = build(model: 'ssc338q', vendor: 'SigmaStar', flash_type: 'nand', size: 128,
               members: { 'rootfs.ubi.ssc338q' => UBI })

    error = assert_raises(Firmware::MissingMember) { fw.generate }
    assert_match(/members: rootfs\.ubi\.ssc338q/, error.message)
  end

  test 'member order in the tarball does not matter' do
    fw = build(model: 'hi3516ev300', vendor: 'HiSilicon', flash_type: 'nand', size: 128,
               members: { 'rootfs.ubi.hi3516ev300' => UBI, 'uImage.hi3516ev300' => KERNEL })
    fw.generate

    assert_equal KERNEL, IO.binread(fw.filepath)[0x100000, KERNEL.bytesize]
  end

  # --- the board is not always the model ---

  test 'members are named for the board rather than the model' do
    # T23N flashes the t23 build: the tarball holds uImage.t23 and
    # rootfs.squashfs.t23. Asking for uImage.t23n found nothing, which is where
    # 31 of the 99 failed downloads in a fortnight came from.
    fw = build(model: 't23n', board: 't23', vendor: 'Ingenic', flash_type: 'nor', size: 8,
               release: 'lite',
               members: { 'uImage.t23' => KERNEL, 'rootfs.squashfs.t23' => SQUASHFS })
    fw.generate

    assert_equal 8.megabytes, File.size(fw.filepath)
    assert_equal KERNEL, IO.binread(fw.filepath)[0x50000, KERNEL.bytesize]
    assert_equal 'openipc-t23n-nor-lite-8mb.bin', fw.filename,
                 'the download is still named for the SoC the user chose'
  end

  test 'a nand board build takes rootfs.ubi named for the board' do
    fw = build(model: 't31x', board: 't31', vendor: 'Ingenic', flash_type: 'nand', size: 128,
               members: { 'uImage.t31' => KERNEL, 'rootfs.ubi.t31' => UBI })
    fw.generate

    assert_equal UBI, IO.binread(fw.filepath)[0x400000, UBI.bytesize]
  end

  # --- a part too big for its slot ---

  test 'a rootfs too large for the flash is refused rather than written past the end' do
    # openipc-hi3516ev200-nor-ultimate-8mb.bin was found in the production
    # cache at 9,465,856 bytes -- 0x250000 plus a 7MB Ultimate rootfs, in a
    # file whose name promises 8MB. IO.binwrite past the end grows the file
    # instead of failing, so nothing objected.
    oversize = "\xC3".b * (8.megabytes - 0x250000 + 1)
    fw = build(model: 'hi3516ev200', vendor: 'HiSilicon', flash_type: 'nor', size: 8,
               members: { 'uImage.hi3516ev200' => KERNEL, 'rootfs.squashfs.hi3516ev200' => oversize })

    error = assert_raises(Firmware::PayloadTooLarge) { fw.generate }
    assert_match(/rootfs/, error.message)
    assert_not File.exist?(fw.filepath), 'an image larger than its flash must not be left to be served'
  end

  test 'a rootfs that exactly fills the flash is still built' do
    exact = "\xC3".b * (8.megabytes - 0x250000)
    fw = build(model: 'hi3516ev200', vendor: 'HiSilicon', flash_type: 'nor', size: 8,
               members: { 'uImage.hi3516ev200' => KERNEL, 'rootfs.squashfs.hi3516ev200' => exact })
    fw.generate

    assert_equal 8.megabytes, File.size(fw.filepath)
  end

  test 'a kernel that would run into the rootfs is refused' do
    oversize = "\xA5".b * (0x250000 - 0x50000 + 1)
    fw = build(model: 'hi3516ev200', vendor: 'HiSilicon', flash_type: 'nor', size: 8,
               members: { 'uImage.hi3516ev200' => oversize, 'rootfs.squashfs.hi3516ev200' => SQUASHFS })

    error = assert_raises(Firmware::PayloadTooLarge) { fw.generate }
    assert_match(/kernel/, error.message)
    assert_not File.exist?(fw.filepath)
  end

  # --- publishing ---

  test 'a failed build leaves neither an image nor a temporary file behind' do
    oversize = "\xC3".b * (8.megabytes - 0x250000 + 1)
    fw = build(model: 'hi3516cv300', vendor: 'HiSilicon', flash_type: 'nor', size: 8,
               members: { 'uImage.hi3516cv300' => KERNEL, 'rootfs.squashfs.hi3516cv300' => oversize })

    assert_raises(Firmware::PayloadTooLarge) { fw.generate }
    assert_not File.exist?(fw.filepath)
    assert_empty leftover_temp_files(fw), 'a half-built image was left in the served directory'
  end

  test 'the image is built beside its destination so publishing is a rename' do
    # In the container Dir.tmpdir is the overlay and public/files is a bind
    # mount, so building in Dir.tmpdir made FileUtils.mv fall back to
    # copy-then-unlink -- writing megabytes into the live filename, where a
    # concurrent request could be served the partial result.
    fw = build(model: 'hi3518ev300', vendor: 'HiSilicon', flash_type: 'nor', size: 8,
               members: { 'uImage.hi3518ev300' => KERNEL, 'rootfs.squashfs.hi3518ev300' => SQUASHFS })

    seen = []
    original = Tempfile.method(:create)
    Tempfile.stub(:create, lambda { |basename, dir = nil|
      seen << dir
      original.call(basename, dir || Dir.tmpdir)
    }) { fw.generate }

    assert_equal [File.dirname(fw.filepath)], seen,
                 'the temporary file must share a filesystem with the destination'
    assert_equal 8.megabytes, File.size(fw.filepath)
  end

  test 'an abandoned build is cleared by the next build of the same image' do
    # A SIGKILL or an OOM leaves the temporary file behind: no rescue and no
    # ensure runs. Debris used to land in Dir.tmpdir, which the container
    # discards on restart; it now lands in the directory nginx serves and
    # nothing prunes, so the next build has to clear it.
    fw = build(model: 'hi3516cv500', vendor: 'HiSilicon', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.hi3516cv500' => KERNEL, 'rootfs.squashfs.hi3516cv500' => SQUASHFS })
    FileUtils.mkdir_p File.dirname(fw.filepath)
    abandoned = File.join(File.dirname(fw.filepath), ".tmp-#{fw.filename}-orphan.bin")
    IO.binwrite(abandoned, 'partial')

    fw.generate

    assert_not File.exist?(abandoned), 'an abandoned build was left in the served directory'
    assert_empty leftover_temp_files(fw)
    assert_equal 8.megabytes, File.size(fw.filepath)
  end

  test 'a build interrupted by a signal does not leave a partial image behind' do
    fw = build(model: 'ssc335', vendor: 'SigmaStar', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.ssc335' => KERNEL, 'rootfs.squashfs.ssc335' => SQUASHFS })

    # Interrupt is not a StandardError, so `rescue StandardError` would have
    # let it through with the temporary file still on disk.
    File.stub(:rename, ->(*) { raise Interrupt }) do
      assert_raises(Interrupt) { fw.generate }
    end

    assert_not File.exist?(fw.filepath)
    assert_empty leftover_temp_files(fw), 'the interrupted build was left behind'
  end

  test 'a cached image that is not its declared size is rebuilt, not served' do
    # openipc-hi3516ev200-nor-ultimate-8mb.bin sat in the production cache at
    # 9,465,856 bytes. public/files is a host mount that outlives deploys and
    # nothing prunes it, so a check on mtimes alone would have gone on serving
    # it until its source tarball happened to change.
    fw = build(model: 'hi3516dv100', vendor: 'HiSilicon', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.hi3516dv100' => KERNEL, 'rootfs.squashfs.hi3516dv100' => SQUASHFS })
    FileUtils.mkdir_p File.dirname(fw.filepath)
    IO.binwrite(fw.filepath, "\x00".b * (8.megabytes + 1024))
    FileUtils.touch(fw.filepath)

    fw.generate

    assert_equal 8.megabytes, File.size(fw.filepath), 'the oversized cache entry was served instead of rebuilt'
    assert_equal SQUASHFS, IO.binread(fw.filepath)[0x250000, SQUASHFS.bytesize]
  end

  test 'the published image is readable by the web server' do
    # Tempfile creates at 0600 and rename keeps the mode, so every image was
    # published readable only by the app's own user. nginx runs as a different
    # one and serves this directory, so /files/ answered 403 for all of them.
    fw = build(model: 'gk7202v300', vendor: 'Goke', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.gk7202v300' => KERNEL, 'rootfs.squashfs.gk7202v300' => SQUASHFS })
    fw.generate

    assert_equal '644', format('%o', File.stat(fw.filepath).mode & 0o777)
  end

  test 'a cached image the web server cannot read is rebuilt, not served' do
    # Thirty images were published at 0600 before the mode was fixed. Counting
    # those as stale mends them on first request rather than by hand.
    fw = build(model: 'ssc30kq', vendor: 'SigmaStar', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.ssc30kq' => KERNEL, 'rootfs.squashfs.ssc30kq' => SQUASHFS })
    fw.generate
    File.chmod(0o600, fw.filepath)

    fw.generate

    assert_equal '644', format('%o', File.stat(fw.filepath).mode & 0o777)
  end

  test 'a half-built image is never readable by the web server' do
    # The temporary file shares the served directory with the image, so it must
    # not be widened until it is complete and has its real name.
    fw = build(model: 'hi3516cv200', vendor: 'HiSilicon', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.hi3516cv200' => KERNEL, 'rootfs.squashfs.hi3516cv200' => SQUASHFS })

    seen = []
    original = File.method(:rename)
    File.stub(:rename, lambda { |from, to|
      seen << (File.stat(from).mode & 0o004)
      original.call(from, to)
    }) { fw.generate }

    assert_equal [0], seen, 'the temporary file was world-readable before it was published'
    assert_equal '644', format('%o', File.stat(fw.filepath).mode & 0o777)
  end

  test 'a second generate reuses the cached image instead of rebuilding it' do
    fw = build(model: 'ssc337', vendor: 'SigmaStar', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.ssc337' => KERNEL, 'rootfs.squashfs.ssc337' => SQUASHFS })
    fw.generate
    first = File.mtime(fw.filepath)

    fw.generate
    assert_equal first, File.mtime(fw.filepath), 'a fresh image was rebuilt anyway'
  end

  # --- concurrency ---

  test 'concurrent requests for one image produce a single complete file' do
    members = { 'uImage.gk7205v300' => KERNEL, 'rootfs.squashfs.gk7205v300' => SQUASHFS }
    builders = Array.new(4) do
      build(model: 'gk7205v300', vendor: 'Goke', flash_type: 'nor', size: 16, release: 'lite',
            members: members)
    end
    FileUtils.rm_f(builders.first.filepath)

    builders.map { |fw| Thread.new { fw.generate } }.each(&:join)

    path = builders.first.filepath
    assert_equal 16.megabytes, File.size(path), 'the published image is not a whole image'
    assert_equal KERNEL, IO.binread(path)[0x50000, KERNEL.bytesize]
    assert_equal SQUASHFS, IO.binread(path)[0x350000, SQUASHFS.bytesize]
    assert_empty leftover_temp_files(builders.first)
  end

  test 'a build waiting on a stuck holder gives up rather than holding the thread' do
    fw = build(model: 'ssc325', vendor: 'SigmaStar', flash_type: 'nor', size: 8, release: 'lite',
               members: { 'uImage.ssc325' => KERNEL, 'rootfs.squashfs.ssc325' => SQUASHFS })
    lock_path = File.join(File.dirname(fw.filepath), ".#{fw.filename}.lock")
    FileUtils.mkdir_p(File.dirname(lock_path))

    holder = File.open(lock_path, File::CREAT | File::RDWR, 0o644)
    holder.flock(File::LOCK_EX)
    previous = Firmware.lock_timeout
    Firmware.lock_timeout = 0.3
    begin
      assert_raises(Firmware::LockTimeout) { fw.generate }
    ensure
      Firmware.lock_timeout = previous
      holder.flock(File::LOCK_UN)
      holder.close
    end
  end

  # --- the image and the instructions must describe one layout ---

  test 'the rootfs lands where the installation page says it will' do
    # Firmware and Camera used to hold separate copies of the partition table,
    # keyed differently -- Firmware on the chip size, Camera on the edition.
    # They agreed only where the two happened to coincide.
    {
      'nor8m' => 8, 'nor16m' => 16, 'nor32m' => 32
    }.each do |flash_type, size|
      fw = build(model: 'hi3518ev200', vendor: 'HiSilicon', flash_type: 'nor', size: size,
                 release: 'lite',
                 members: { 'uImage.hi3518ev200' => KERNEL, 'rootfs.squashfs.hi3518ev200' => SQUASHFS })
      fw.generate

      camera = Camera.new(flash_type: flash_type, firmware_version: 'lite', soc: fw_soc('HiSilicon'))
      offset = camera.rootfs_offset.to_i(16)

      assert_equal SQUASHFS, IO.binread(fw.filepath)[offset, SQUASHFS.bytesize],
                   "#{flash_type}: the page points at 0x#{offset.to_s(16)}, the image does not"
    end
  end

  test 'the vendors that keep the 8MB offsets keep them in both places' do
    %w[SigmaStar Ingenic].each do |vendor|
      fw = build(model: 'ssc338q', vendor: vendor, flash_type: 'nor', size: 16, release: 'lite',
                 members: { 'uImage.ssc338q' => KERNEL, 'rootfs.squashfs.ssc338q' => SQUASHFS })
      fw.generate

      camera = Camera.new(flash_type: 'nor16m', firmware_version: 'lite', soc: fw_soc(vendor))
      assert_equal '0x250000', camera.rootfs_offset, "#{vendor} lost its 8MB rootfs offset"
      assert_equal SQUASHFS, IO.binread(fw.filepath)[0x250000, SQUASHFS.bytesize],
                   "#{vendor}: the image does not match the page"
    end
  end

  # --- the index going away must not refuse an image that is already built ---

  # Stands in for a Soc whose parts cannot be reached: no index, GitHub
  # unreachable, bytes that did not match. ReleaseCache raises Unavailable for
  # all three and Soc passes it straight through.
  class UnreachableSoc < StubSoc
    def uboot_file
      raise ReleaseCache::Unavailable, 'no release index: /srv/github-releases/.index.json is not there'
    end
  end

  # The same SoC, with its parts out of reach.
  def unreachable
    UnreachableSoc.new(model: 'ts3516ev300', vendor: 'HiSilicon', uboot_file: nil, linux_file: nil)
  end

  # A built, complete 8MB NOR image, and a Firmware pointing at it whose parts
  # cannot be reached.
  def built_then_unreachable
    good = build(model: 'ts3516ev300', vendor: 'HiSilicon', flash_type: 'nor', size: 8,
                 release: 'lite',
                 members: { 'uImage.ts3516ev300' => KERNEL, 'rootfs.squashfs.ts3516ev300' => SQUASHFS })
    good.generate
    [good, Firmware.new(size: 8, flash_type: 'nor', release: 'lite', soc: unreachable)]
  end

  test 'an image already built is served when its parts cannot be reached' do
    # Verified on dev before this changed: a complete, current 8MB image was
    # refused with "no release index" because generate resolved both sources
    # before asking whether it needed them.
    good, offline = built_then_unreachable
    assert_equal 8.megabytes, File.size(good.filepath)

    assert_nil offline.generate
    assert_equal 8.megabytes, File.size(offline.filepath)
  end

  test 'nothing built means the failure still reaches the caller' do
    offline = Firmware.new(size: 8, flash_type: 'nor', release: 'lite',
                           soc: UnreachableSoc.new(model: 'ts3516ev999', vendor: 'HiSilicon',
                                                   uboot_file: nil, linux_file: nil))

    assert_raises(ReleaseCache::Unavailable) { offline.generate }
  end

  test 'an image of the wrong size is not served in place of building one' do
    # The 9,465,856-byte 8MB image that was live on this host is why right_size?
    # exists. Unreachable parts must not become a reason to serve it.
    good, offline = built_then_unreachable
    IO.binwrite(good.filepath, 'x' * 1024)

    assert_raises(ReleaseCache::Unavailable) { offline.generate }
  end

  test 'an image the web server cannot read is not served either' do
    good, offline = built_then_unreachable
    File.chmod(0o600, good.filepath)

    assert_raises(ReleaseCache::Unavailable) { offline.generate }
  end

  test 'an asset upstream does not publish is still refused, cached image or not' do
    # UnknownAsset is permanent, not a bad minute. A leftover image for a name
    # that has since been aliased away is exactly what must not be served.
    good, = built_then_unreachable
    assert_equal 8.megabytes, File.size(good.filepath)

    gone = Class.new(StubSoc) do
      def uboot_file
        raise ReleaseCache::UnknownAsset, '"openipc.gone-nor-lite.tgz" is not in the release index'
      end
    end.new(model: 'ts3516ev300', vendor: 'HiSilicon', uboot_file: nil, linux_file: nil)

    assert_raises(ReleaseCache::UnknownAsset) do
      Firmware.new(size: 8, flash_type: 'nor', release: 'lite', soc: gone).generate
    end
  end
end
