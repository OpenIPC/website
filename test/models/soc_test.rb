# frozen_string_literal: true

require 'test_helper'

# Soc.find is overridden to resolve the urlname slug that to_param produces.
# These pin the half that was missing: what it does when nothing matches.
class SocTest < ActiveSupport::TestCase
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300')
  end

  test 'generates a slug from the model and addresses the record by it' do
    assert_equal 'ts3516ev300', @soc.urlname
    assert_equal 'ts3516ev300', @soc.to_param
    assert_equal @soc, Soc.find('ts3516ev300')
  end

  test 'still finds by id, for old links and the admin forms' do
    assert_equal @soc, Soc.find(@soc.id)
  end

  test 'raises RecordNotFound for a slug that does not exist' do
    # /cameras/vendors/ingenic/socs/t31 used to answer 500 here: find returned
    # nil and Firmware#generate then called uboot_file on it. There is no SoC
    # called plain "t31" -- the real ones are t31a, t31x, t31n and so on.
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find('no-such-soc') }
  end

  test 'raises RecordNotFound for a blank identifier' do
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find(nil) }
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find('') }
  end

  test 'find_by_param answers nil instead of raising' do
    assert_equal @soc, Soc.find_by_param('ts3516ev300')
    assert_nil Soc.find_by_param('no-such-soc')
    assert_nil Soc.find_by_param(nil)
    assert_nil Soc.find_by_param('')
  end

  # --- which build a SoC actually flashes ---

  test 'board falls back to the model when nothing says otherwise' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3516CV500',
                      linux_filename: 'openipc.ts3516cv500-nor-lite.tgz')
    assert_equal 'ts3516cv500', soc.board
  end

  test 'board follows linux_filename where the build is shared across a family' do
    # Ingenic ships one build per family: T23N, like T31X and the rest, flashes
    # a tarball named for the family, and its members are named to match. The
    # board used to be guessed from the model with exceptions hardcoded for
    # t31 and t40 only, so T23N asked for openipc.t23n-nor-lite.tgz -- a file
    # that has never been published.
    soc = Soc.create!(vendor: @vendor, model: 'T23N',
                      linux_filename: 'openipc.t23-nor-lite.tgz')
    assert_equal 't23', soc.board
    assert_equal 'openipc.t23-nor-lite.tgz', soc.linux_filename_for('lite', 'nor')
  end

  test 'board handles a build named for something other than the family' do
    # AK3916EV301 runs the AK3918EV200 build outright, which no rule derived
    # from the model string could ever produce.
    soc = Soc.create!(vendor: @vendor, model: 'AK3916EV301',
                      linux_filename: 'openipc.ak3918ev200-nor-lite.tgz')
    assert_equal 'ak3918ev200', soc.board
  end

  test 'board copes with a hyphenated board name' do
    soc = Soc.create!(vendor: @vendor, model: 'S3L',
                      linux_filename: 'openipc.ambarella-s3l-nor-lite.tgz')
    assert_equal 'ambarella-s3l', soc.board
  end

  test 'board ignores a filename that is not in the published scheme' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV200',
                      linux_filename: 'openipc.ts3516ev200-br.tgz')
    assert_equal 'ts3516ev200', soc.board, 'a legacy name must not be parsed for a board'
  end

  test 'the family rule still answers for a legacy filename' do
    # db/seeds.rb carries the pre-2023 openipc.<soc>-br.tgz name for all 48 of
    # its entries, so a fresh install has nothing modern to read. Without the
    # fallback T31X would ask for a t31x build, which upstream has never
    # published -- the very regression this change exists to fix.
    %w[T31X T40XP T30L T23N].each do |model|
      soc = Soc.create!(vendor: @vendor, model: model,
                        linux_filename: "openipc.#{model.downcase}-br.tgz")
      assert_equal model.downcase[0, 3], soc.board, "#{model} lost its family build"
    end
  end

  test 'the family rule leaves an unrelated model alone' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3518EV300', linux_filename: '')
    assert_equal 'ts3518ev300', soc.board
  end

  test 'linux_file answers each release and flash type it is asked for' do
    # `@linux_file ||=` returned the first call's path for every later one, so
    # asking one Soc for two variants handed back the same tarball twice.
    soc = Soc.create!(vendor: @vendor, model: 'TS3519DV500',
                      linux_filename: 'openipc.ts3519dv500-nor-lite.tgz')

    assert_equal 'openipc.ts3519dv500-nor-lite.tgz', soc.linux_filename_for('lite', 'nor')
    assert_equal 'openipc.ts3519dv500-nor-ultimate.tgz', soc.linux_filename_for('ultimate', 'nor')
    assert_equal 'openipc.ts3519dv500-nand-ultimate.tgz', soc.linux_filename_for('ultimate', 'nand')
  end

  # --- SoC aliases ---
  #
  # Upstream retires a chip by building the board it is identical to and
  # pointing the retired id at it. The site learns the map from the index.

  def with_index(aliases: {}, assets: [])
    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    File.write(File.join(root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z', 'aliases' => aliases,
                             'assets' => assets.to_h { |n| [n, { 'size' => 1 }] }))
    ReleaseIndex.reset!
    yield
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end

  def with_aliases(aliases, &)
    with_index(aliases: aliases, &)
  end

  test 'board follows the alias to the board upstream actually builds' do
    # GK7205V210 has not been built since 2026-06-07; it is firmware-identical
    # to GK7205V200 and served from it. Asking for openipc.gk7205v210-*.tgz is a
    # dead download, not a stale one -- upstream deleted nothing, it simply
    # stopped publishing a second copy under the old name.
    soc = Soc.create!(vendor: @vendor, model: 'TS7205V210',
                      linux_filename: 'openipc.ts7205v210-nor-lite.tgz')

    with_aliases('ts7205v210' => 'ts7205v200') do
      assert_equal 'ts7205v200', soc.board
      assert_equal 'openipc.ts7205v200-nor-lite.tgz', soc.linux_filename_for('lite', 'nor')
    end
  end

  test 'the alias reaches the tarball members as well as the tarball' do
    # The board names what is inside the archive too. Substituting only the
    # filename would fetch the right tarball and then fail to find a kernel in
    # it, which is a worse failure than the one being fixed.
    soc = Soc.create!(vendor: @vendor, model: 'TS7205V210',
                      linux_filename: 'openipc.ts7205v210-nor-lite.tgz')

    with_aliases('ts7205v210' => 'ts7205v200') do
      assert_equal 'uImage.ts7205v200', soc.kernel_file
      assert_equal 'rootfs.squashfs.ts7205v200', soc.rootfs_file
    end
  end

  test 'a board with no alias is left exactly as it was' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3516CV500',
                      linux_filename: 'openipc.ts3516cv500-nor-lite.tgz')

    with_aliases('ts7205v210' => 'ts7205v200') do
      assert_equal 'ts3516cv500', soc.board
    end
  end

  test 'an alias pointing a board at itself is ignored' do
    # It would say nothing, and taking it literally is how a map like this
    # becomes a loop.
    soc = Soc.create!(vendor: @vendor, model: 'TS3516CV500',
                      linux_filename: 'openipc.ts3516cv500-nor-lite.tgz')

    with_aliases('ts3516cv500' => 'ts3516cv500') do
      assert_equal 'ts3516cv500', soc.board
    end
  end

  test 'no index leaves the board as the column names it' do
    # Every SoC without an alias is unaffected by the index being unreadable,
    # and this is what all of them did before the map existed.
    soc = Soc.create!(vendor: @vendor, model: 'TS7205V210',
                      linux_filename: 'openipc.ts7205v210-nor-lite.tgz')

    ReleaseIndex.reset!
    assert_equal 'ts7205v210', soc.board
  end

  # --- what is missing, and saying which ---

  test 'a SoC with firmware but no bootloader row reports exactly that' do
    # Every Xiongmai part and the whole GK7102 family: OpenIPC builds firmware
    # for them and publishes no u-boot, so the flash image cannot be assembled
    # however much of the rest exists.
    soc = Soc.create!(vendor: @vendor, model: 'TS550', uboot_filename: '',
                      linux_filename: 'openipc.ts550-nor-lite.tgz')

    with_index(assets: ['openipc.ts550-nor-lite.tgz']) do
      assert_predicate soc, :firmware_published?
      assert_not_predicate soc, :bootloader_published?
    end
  end

  test 'a bootloader named but not published counts as missing' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3536DV100',
                      uboot_filename: 'u-boot-ts3536dv100-universal.bin',
                      linux_filename: 'openipc.ts3536dv100-nor-lite.tgz')

    with_index(assets: ['openipc.ts3536dv100-nor-lite.tgz']) do
      assert_not_predicate soc, :bootloader_published?
    end
  end

  test 'both published is the ordinary answer' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV200',
                      uboot_filename: 'u-boot-ts3516ev200-universal.bin',
                      linux_filename: 'openipc.ts3516ev200-nor-lite.tgz')

    with_index(assets: ['openipc.ts3516ev200-nor-lite.tgz',
                        'u-boot-ts3516ev200-universal.bin']) do
      assert_predicate soc, :bootloader_published?
      assert_predicate soc, :firmware_published?
    end
  end

  test 'firmware_published? looks at every edition and flash type' do
    # NAND-only and ultimate-only boards are both real, so asking about nor
    # lite alone would call them unsupported.
    soc = Soc.create!(vendor: @vendor, model: 'TS3519DV500',
                      linux_filename: 'openipc.ts3519dv500-nor-lite.tgz')

    with_index(assets: ['openipc.ts3519dv500-nand-ultimate.tgz']) do
      assert_predicate soc, :firmware_published?
    end
  end

  test 'no index never claims a bootloader is missing' do
    # Not being able to see the index is not evidence of absence, and telling a
    # visitor their SoC has no bootloader on that basis would be worse than
    # saying nothing.
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV200',
                      uboot_filename: 'u-boot-ts3516ev200-universal.bin',
                      linux_filename: 'openipc.ts3516ev200-nor-lite.tgz')

    ReleaseIndex.reset!
    assert_predicate soc, :bootloader_published?
  end

  test 'a blank bootloader column is missing whatever the index says' do
    soc = Soc.create!(vendor: @vendor, model: 'TS550', uboot_filename: '',
                      linux_filename: 'openipc.ts550-nor-lite.tgz')

    ReleaseIndex.reset!
    assert_not_predicate soc, :bootloader_published?
  end

  # --- which editions are offered ---

  test 'only the editions upstream publishes are offered' do
    # Ultimate does not exist for 42 of the SoCs this site lists and fabricator
    # exists for none of them, yet all three were offered for every SoC.
    soc = Soc.create!(vendor: @vendor, model: 'TS377D',
                      linux_filename: 'openipc.ts377d-nor-lite.tgz')

    with_index(assets: ['openipc.ts377d-nor-lite.tgz']) do
      assert_equal %w[lite], soc.available_releases('nor')
      assert_empty soc.available_releases('nand')
      assert_equal %w[lite], soc.offerable_releases
    end
  end

  test 'an edition the site has never heard of is offered anyway' do
    # neo shipped for seven boards and nothing here could reach it, because the
    # list was a constant. Reading the names means the next one does not wait
    # on a deploy.
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV301',
                      linux_filename: 'openipc.ts3516ev301-nor-lite.tgz')

    with_index(assets: ['openipc.ts3516ev301-nor-lite.tgz',
                        'openipc.ts3516ev301-nor-neo.tgz',
                        'openipc.ts3516ev301-nor-zephyr.tgz']) do
      assert_equal %w[lite neo zephyr], soc.offerable_releases
    end
  end

  test 'editions are ordered lite, ultimate, neo, then whatever else' do
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV302',
                      linux_filename: 'openipc.ts3516ev302-nor-lite.tgz')

    with_index(assets: ['openipc.ts3516ev302-nor-neo.tgz',
                        'openipc.ts3516ev302-nor-lite.tgz',
                        'openipc.ts3516ev302-nor-ultimate.tgz']) do
      assert_equal %w[lite ultimate neo], soc.available_releases('nor')
    end
  end

  test 'nand and nor are answered separately' do
    # Five NAND boards are lite-only while the page forced Ultimate for NAND,
    # and most SoCs have no NAND build at all.
    soc = Soc.create!(vendor: @vendor, model: 'TS1106',
                      linux_filename: 'openipc.ts1106-nand-lite.tgz')

    with_index(assets: ['openipc.ts1106-nand-lite.tgz']) do
      assert_empty soc.available_releases('nor')
      assert_equal %w[lite], soc.available_releases('nand')
      assert_equal({ 'nor' => [], 'nand' => %w[lite] }, soc.release_availability)
    end
  end

  test 'the alias decides which board the editions are read from' do
    soc = Soc.create!(vendor: @vendor, model: 'TS7205V210',
                      linux_filename: 'openipc.ts7205v210-nor-lite.tgz')

    with_index(aliases: { 'ts7205v210' => 'ts7205v200' },
               assets: ['openipc.ts7205v200-nor-lite.tgz',
                        'openipc.ts7205v200-nor-ultimate.tgz']) do
      assert_equal %w[lite ultimate], soc.offerable_releases
    end
  end

  test 'no index offers the editions it always used to' do
    # An empty menu would be worse than the problem being fixed.
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV303',
                      linux_filename: 'openipc.ts3516ev303-nor-lite.tgz')

    ReleaseIndex.reset!
    assert_equal Soc::RELEASE_ORDER, soc.available_releases('nor')
  end

  test 'fw_url names the release it was asked for' do
    # It returned the lite filename for every version, in the pre-2023 naming.
    soc = Soc.create!(vendor: @vendor, model: 'TS3516EV304',
                      linux_filename: 'openipc.ts3516ev304-nor-lite.tgz')

    assert_match(/openipc\.ts3516ev304-nor-ultimate\.tgz\z/, soc.fw_url('ultimate'))
    assert_match(/openipc\.ts3516ev304-nand-lite\.tgz\z/, soc.fw_url('lite', 'nand'))
  end
end
