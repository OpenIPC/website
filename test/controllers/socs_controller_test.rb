# frozen_string_literal: true

require 'test_helper'

class SocsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    # status drives image_tag("stage-#{status}.svg"), which sprockets raises on
    # if the file is missing, and the blank filenames keep instructable? false
    # so the row renders without building an installation link.
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300', status: 'done',
                       uboot_filename: '', linux_filename: '')
  end

  # show_exceptions is off in this environment, so the exception reaches the
  # test rather than being rendered. In production RescueHandler turns
  # RecordNotFound into public/404.html; what matters here is that it is
  # RecordNotFound at all, and not the NoMethodError on nil that Firmware
  # #generate used to raise -- which surfaced as a 500.
  test 'an unknown SoC slug is not found rather than a server error' do
    assert_raises(ActiveRecord::RecordNotFound) do
      get "/cameras/vendors/#{@vendor.to_param}/socs/no-such-soc/download_full_image",
          params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }
    end
  end

  test 'an unknown vendor slug is not found either' do
    assert_raises(ActiveRecord::RecordNotFound) do
      get '/cameras/vendors/no-such-vendor'
    end
  end

  # The SoC index is a vendor filter and nothing else. The template calls
  # `render @socs` unconditionally, so the action leaving it unset answered 500
  # for anyone who reached /cameras/socs without one.
  test 'the SoC index sends a request with no vendor to the featured page' do
    get '/cameras/socs'

    assert_redirected_to '/supported-hardware/featured'
  end

  # No test renders a page here. The layout asks the pipeline for
  # application.css, app/assets/builds/ is gitignored, and the workflow runs
  # `bin/rails test` without building assets first -- so a full render errors
  # with "The asset \"application.css\" is not present in the asset pipeline"
  # for reasons that have nothing to do with the code under test. The
  # rendering path is checked against the running site instead.

  test 'the SoC index does not find a vendor that does not exist' do
    assert_raises(ActiveRecord::RecordNotFound) do
      get '/cameras/socs', params: { vendor: 'no-such-vendor' }
    end
  end

  # --- telling the visitor which half is missing ---

  test 'a SoC with no bootloader says so instead of "this firmware does not exist"' do
    # Every Xiongmai part is like this, and the whole GK7102 family: firmware is
    # published and linked on the SoC page, and there is no u-boot to start a
    # flash image with. Reporting that as a missing firmware sends the visitor
    # looking for a fault that is not there.
    soc = Soc.create!(vendor: @vendor, model: 'TS550', status: 'done',
                      uboot_filename: '', linux_filename: 'openipc.ts550-nor-lite.tgz')

    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    File.write(File.join(root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z', 'aliases' => {},
                             'assets' => { 'openipc.ts550-nor-lite.tgz' => { 'size' => 1 } }))
    ReleaseIndex.reset!

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}/download_full_image",
        params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }

    assert_response :redirect
    assert_match(/does not publish a bootloader/, flash[:alert])
    assert_no_match(/does not exist/, flash[:alert])
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end

  test 'an edition upstream does not build keeps the plain does-not-exist wording' do
    # Both halves of this SoC are published; Ultimate for it is not. That is
    # the case the plain wording is right about, and it is the only one left
    # for it. The u-boot is served from a stub so the request gets past it to
    # the failure being tested.
    soc = Soc.create!(vendor: @vendor, model: 'TS9999', status: 'done',
                      uboot_filename: 'u-boot-ts9999-universal.bin',
                      linux_filename: 'openipc.ts9999-nor-lite.tgz')

    root = Dir.mktmpdir
    cache = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    File.write(File.join(root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z', 'aliases' => {},
                             'assets' => { 'u-boot-ts9999-universal.bin' => { 'size' => 4 },
                                           'openipc.ts9999-nor-lite.tgz' => { 'size' => 4 } }))
    ReleaseIndex.reset!
    ReleaseCache.root = cache
    ReleaseCache.downloader = lambda { |_url, dest|
      IO.binwrite(dest, 'boot')
      Digest::SHA256.hexdigest('boot')
    }

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}/download_full_image",
        params: { flash_size: 8, flash_type: 'nor', fw_release: 'ultimate' }

    assert_response :redirect
    assert_equal 'This firmware does not exist.', flash[:alert]
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    ReleaseCache.root = nil
    ReleaseCache.downloader = nil
    FileUtils.remove_entry(root) if root
    FileUtils.remove_entry(cache) if cache
  end

  test 'a SoC with nothing published says so rather than promising a bundle' do
    # MSC313E: a bootloader named that upstream does not publish, and no
    # firmware either. The bootloader wording would tell this visitor to go and
    # find a bundle on the SoC page that is not there.
    soc = Soc.create!(vendor: @vendor, model: 'TS313E', status: 'done',
                      uboot_filename: 'u-boot-ts313e-universal.bin',
                      linux_filename: 'openipc.ts313e-nor-lite.tgz')

    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    File.write(File.join(root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z',
                             'aliases' => {}, 'assets' => {}))
    ReleaseIndex.reset!

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}/download_full_image",
        params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }

    assert_response :redirect
    assert_equal 'OpenIPC does not publish firmware for this SoC yet.', flash[:alert]
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end

  test 'an unknown SoC slug on the show page is not found rather than a server error' do
    # /cameras/vendors/rockchip/socs/rv1106 answered 500 in production: RV1106
    # has no row here, show used the nil-returning finder, and the next line
    # called .vendor on nil.
    assert_raises(ActiveRecord::RecordNotFound) do
      get "/cameras/vendors/#{@vendor.to_param}/socs/no-such-soc"
    end
  end

  # --- the wizard renders the chip the visitor chose ---
  #
  # These render the whole installation page. That used to be impossible here --
  # app/assets/builds/ is gitignored and sprockets raises on a missing
  # application.css -- but CI now builds the assets before `bin/rails test`, and
  # web_interface_test.rb and majestic_endpoints_test.rb already rely on it.

  # Stub the release index with exactly these assets for the duration of the
  # block. The installation page asks it what upstream publishes, and
  # use_published_release! moves the visitor off an edition that is not there.
  def with_release_index(*filenames)
    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    write_index(root, filenames)
    ReleaseIndex.reset!
    yield
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end

  def write_index(root, filenames)
    assets = filenames.to_h { |name| [name, { 'size' => 1 }] }
    File.write(File.join(root, '.index.json'),
               JSON.generate('generated_at' => '2026-08-24T00:00:00Z', 'aliases' => {}, 'assets' => assets))
  end

  # Both editions on both flash types, so use_published_release! leaves the
  # submitted choice alone and the assertions are about flash type and nothing
  # else.
  def every_edition_for(soc)
    %w[nor nand].product(%w[lite ultimate]).map { |flash, release| "openipc.#{soc.board}-#{flash}-#{release}.tgz" }
  end

  def instructable_soc(model)
    Soc.create!(vendor: @vendor, model:, status: 'done', load_address: '0x82000000',
                uboot_filename: "u-boot-#{model.downcase}-universal.bin",
                linux_filename: "openipc.#{model.downcase}-nor-lite.tgz")
  end

  # partition_layout is left out unless a test asks for one, so the default path
  # -- which is what the form sends for every chip whose layout is its own -- is
  # what the rest of these tests keep exercising.
  def submit(soc, flash_type, firmware_version: 'lite', partition_layout: nil, locale: nil)
    camera = { flash_type:, firmware_version:,
               network_interface: 'eth', sd_card_slot: 'nosd',
               camera_ip_address: '192.168.1.10',
               server_ip_address: '192.168.1.254',
               camera_mac_address: '00:11:22:33:44:55' }
    camera[:partition_layout] = partition_layout if partition_layout

    put "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}#{"?locale=#{locale}" if locale}",
        params: { camera: }
    assert_response :success
  end

  # --- the chip and the layout, separately ---

  # The report this second menu exists for. A 16MB camera with a ruined overlay,
  # reflashed from the 8MB entry, came back exactly as broken: the erase stopped
  # at 0x800000 while rootfs_data -- `-` in every mtdparts, so to the end of the
  # device -- ran to 0x1000000, and /init mounts a jffs2 it finds rather than
  # reformatting it. The erase has to span the chip whatever layout goes inside.
  test 'the 8MB layout on a 16MB chip erases the whole chip' do
    soc = instructable_soc('TS3516EVE00')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m', partition_layout: 'nor8m')

      assert_match 'sf erase 0x0 0x1000000', response.body
      assert_no_match(/sf erase 0x0 0x800000/, response.body)
      # One image, laid out the 8MB way and sized for the chip it goes on.
      assert_match 'openipc-ts3516eve00-nor-lite-16mb-parts8m.bin', response.body
      assert_match 'layout=8', response.body
      # ...and the by-parts block underneath agrees with it.
      assert_match 'run uknor8m; run urnor8m', response.body
      assert_match 'sf erase 0x750000 0x8b0000', response.body
    end
  end

  test 'a chip whose layout is its own is named and flashed exactly as before' do
    soc = instructable_soc('TS3516EVE10')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m')

      assert_match 'openipc-ts3516eve10-nor-lite-16mb.bin', response.body
      assert_no_match(/parts8m/, response.body)
      assert_match 'sf erase 0x0 0x1000000', response.body
      assert_match 'run uknor16m; run urnor16m', response.body
    end
  end

  test 'the 16MB layout is refused on an 8MB chip, and the page says so' do
    soc = instructable_soc('TS3516EVE20')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor8m', partition_layout: 'nor16m')

      assert_match 'The 16MB partition layout needs a 16MB chip', response.body
      assert_match 'sf erase 0x0 0x800000', response.body
      assert_match 'openipc-ts3516eve20-nor-lite-8mb.bin', response.body
    end
  end

  # The rootfs partition is what Ultimate does not fit in, and that is the
  # layout's doing rather than the chip's: 5120KB is 5120KB on a 32MB part too.
  test 'Ultimate is refused by the 8MB layout even on a chip with room' do
    soc = instructable_soc('TS3516EVE30')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m', firmware_version: 'ultimate', partition_layout: 'nor8m')

      assert_match 'The 8MB partition layout leaves 5MB for the rootfs', response.body
      assert_match 'openipc-ts3516eve30-nor-lite-16mb-parts8m.bin', response.body
    end
  end

  test 'the permanent link carries the layout so it can be reopened' do
    soc = instructable_soc('TS3516EVE40')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m', partition_layout: 'nor8m')

      assert_match(/part=nor8m/, response.body)
    end
  end

  test 'a 16MB submission gets the 16MB layout, not the SoC default' do
    # update guarded the default_flash_chip fallback on params[:rom], which is
    # how `show` receives a choice and not how this action does: the form PUTs
    # camera[flash_type] and never sends rom, so the guard was always true and
    # every visitor was silently moved to nor8m. @flash_type_command was read
    # before that happened, so the page said `run urnor16m` -- which writes the
    # rootfs to 0x350000..0xd50000 -- and then erased from the 8MB overlay
    # offset, 733,184 bytes inside it. Issue #60, by another route.
    soc = instructable_soc('TS3516EV200')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m')

      assert_match 'run setnor16m', response.body
      assert_match 'run uknor16m; run urnor16m', response.body
      assert_match 'sf erase 0xD50000', response.body
      assert_no_match(/sf erase 0x750000/, response.body)
    end
  end

  test 'an 8MB submission still gets the 8MB layout' do
    soc = instructable_soc('TS3518EV200')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor8m')

      assert_match 'run uknor8m; run urnor8m', response.body
      assert_match 'sf erase 0x750000', response.body
    end
  end

  test 'a NAND submission is not turned into a NOR one' do
    # default_flash_chip answers nor8m for any SoC with a NOR build, so this
    # combination rendered `run uknand; run urnand` followed by an `sf erase`:
    # Camera#nand? was false, and the erase is meaningless on a UBI partition.
    soc = instructable_soc('TS3516CV500')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nand')

      assert_match 'run setnand', response.body
      assert_match 'run uknand; run urnand', response.body
      assert_no_match(/sf erase/, response.body)
    end
  end

  test 'a submission with no flash type still falls back to what the SoC has' do
    # What the guard was there for: rv1109 and rv1126 publish only a NAND image,
    # their NOR sizes are disabled in the menu, and nor8m is the wrong place to
    # start. Nothing on NOR here, so default_flash_chip answers nand -- and the
    # commands have to follow it, which is why @flash_type_command is read after
    # the fallback rather than before. It used to render a bare `run set`.
    soc = Soc.create!(vendor: @vendor, model: 'TS1126', status: 'done',
                      load_address: '0x82000000',
                      uboot_filename: 'u-boot-ts1126-universal.bin',
                      linux_filename: 'openipc.ts1126-nand-ultimate.tgz')

    with_release_index('openipc.ts1126-nand-ultimate.tgz') do
      submit(soc, '', firmware_version: 'ultimate')

      assert_match 'run setnand', response.body
      assert_match 'run uknand; run urnand', response.body
    end
  end

  test '8MB with only an Ultimate build says so instead of rendering it silently' do
    # Ultimate does not fit 8MB. The downgrade to Lite is conditional on a Lite
    # build existing, which is right -- hi3516cv6xx and hi3519dv500 publish
    # Ultimate and nothing else, and downgrading regardless would name a tarball
    # upstream never built. But with nothing to fall back to the page went on to
    # render `run urnor8m`, which erases a 5120k rootfs partition, for a rootfs
    # that cannot fit in it, and said nothing about it.
    soc = instructable_soc('TS3519DV500')

    with_release_index("openipc.#{soc.board}-nor-ultimate.tgz") do
      submit(soc, 'nor8m', firmware_version: 'ultimate')

      assert_match(/does not fit an 8MB flash chip/, response.body)
    end
  end

  # --- the printenv hint names the variables the page just used ---

  test 'the printenv hint names the NAND variables when NAND was chosen' do
    # It was a fixed `uknor*, urnor*, setnor*` in all ten locales, so a NAND
    # reader was pointed at three variables that appear nowhere in their own
    # instructions and none of the three that do. OpenIPC/website#63.
    soc = instructable_soc('TS3516DV300')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nand')

      assert_match '<code>uknand</code>, <code>urnand</code>, <code>setnand</code>', response.body
      assert_no_match(/uknor/, response.body)
    end
  end

  test 'the printenv hint names the chip-sized NOR variables' do
    soc = instructable_soc('TS3519AV100')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m')

      assert_match '<code>uknor16m</code>, <code>urnor16m</code>, <code>setnor16m</code>', response.body
    end
  end

  test 'a 32MB chip is pointed at the nor16m variables it was told to run' do
    # The controller rewrites nor32m to the nor16m command set -- there is no
    # mtdpartsnor32m upstream -- and the hint has to follow that rewrite rather
    # than name a variable no bootloader defines.
    soc = instructable_soc('TS3521DV100')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor32m')

      assert_match 'run uknor16m; run urnor16m', response.body
      assert_match '<code>uknor16m</code>, <code>urnor16m</code>, <code>setnor16m</code>', response.body
      assert_no_match(/uknor32m/, response.body)
    end
  end

  test 'a flash chip this site does not know is treated as no choice at all' do
    # Nothing calls valid? on a Camera and every field arrives from the request,
    # so before this the submitted string went straight into the rendered
    # commands: `run setnor64m`, `run uknor64m; run urnor64m`, and a printenv
    # hint naming three variables no bootloader defines -- while flash_size_hex
    # and friends quietly fell through to their 8MB branch alongside them.
    soc = instructable_soc('TS3520DV200')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor64m')

      assert_no_match(/nor64m/, response.body)
      assert_match 'run uknor8m; run urnor8m', response.body
      assert_match '<code>uknor8m</code>, <code>urnor8m</code>, <code>setnor8m</code>', response.body
    end
  end

  # --- the size rule sees the edition that is actually rendered ---

  test 'the 8MB rule is applied after the edition has settled, not before' do
    # A SoC published as Ultimate and nothing else. A `lite` submission arrives
    # as Lite, use_published_release! moves it to Ultimate because that is all
    # there is, and the 8MB rule had already looked and seen Lite -- so nor8m +
    # Ultimate rendered with nothing said about it. The same
    # read-before-it-settled mistake the flash type itself had.
    soc = instructable_soc('TS9911EV300')

    with_release_index("openipc.#{soc.board}-nor-ultimate.tgz") do
      submit(soc, 'nor8m', firmware_version: 'lite')

      assert_match(/does not fit an 8MB flash chip/, response.body)
    end
  end

  test 'a SoC with no NOR firmware at all is not told to buy a larger NOR chip' do
    # rv1109 and rv1126 are like this. A larger chip does not help: there is no
    # NOR build for the part at any size.
    soc = Soc.create!(vendor: @vendor, model: 'TS1109', status: 'done',
                      load_address: '0x82000000',
                      uboot_filename: 'u-boot-ts1109-universal.bin',
                      linux_filename: 'openipc.ts1109-nand-ultimate.tgz')

    with_release_index('openipc.ts1109-nand-ultimate.tgz') do
      submit(soc, 'nor8m', firmware_version: 'ultimate')

      assert_match(/publishes no NOR firmware for this SoC/, response.body)
      assert_no_match(/needs a larger chip/, response.body)
    end
  end

  test 'a flash message is rendered once, at the severity it was raised with' do
    # display_flashes mapped everything but alert/error to alert-info, and
    # update.html.erb separately iterated flash.each -- flash.discard marks a
    # key for sweeping without removing it from the current request -- so the
    # page carried the same sentence twice, once calm blue and once red.
    soc = instructable_soc('TS9912EV300')

    with_release_index("openipc.#{soc.board}-nor-ultimate.tgz") do
      submit(soc, 'nor8m', firmware_version: 'ultimate')

      assert_equal 1, response.body.scan(/does not fit an 8MB flash chip/).size
      assert_select 'div.alert-danger', /does not fit an 8MB flash chip/
      assert_select 'div.alert-info', { count: 0, text: /does not fit an 8MB flash chip/ }
    end
  end

  # --- a chip with nothing published for it says so ---

  test 'NAND on a SoC with no NAND build says so instead of naming the tarball' do
    # use_published_release! moves the visitor onto a published edition, but
    # returns without a word when there is none to move to -- `return nil if
    # available.empty?`. That silence was unreachable while every submission was
    # forced onto default_flash_chip, which picks a flash type that has builds.
    # ~110 SoCs here have no NAND build; this rendered `run uknand; run urnand`,
    # a bundle link and a download link for all of them.
    soc = instructable_soc('TS3516EV100')

    with_release_index("openipc.#{soc.board}-nor-lite.tgz") do
      submit(soc, 'nand')

      assert_match(/publishes no NAND firmware for this SoC/, response.body)
    end
  end

  test 'a larger NOR size on a NAND-only SoC says so too, not just 8MB Ultimate' do
    # The first cut of this guard sat inside the 8MB rule, so it only fired for
    # nor8m plus Ultimate. Every other edition and every other NOR size on the
    # same SoC still rendered NOR instructions in silence.
    soc = Soc.create!(vendor: @vendor, model: 'TS1127', status: 'done',
                      load_address: '0x82000000',
                      uboot_filename: 'u-boot-ts1127-universal.bin',
                      linux_filename: 'openipc.ts1127-nand-ultimate.tgz')

    with_release_index('openipc.ts1127-nand-ultimate.tgz') do
      submit(soc, 'nor16m')

      assert_match(/publishes no NOR firmware for this SoC/, response.body)
    end
  end

  test 'an unreadable release index is not reported as nothing being published' do
    # available_releases answers the known list rather than [] when the index
    # cannot be read, so an index outage must not turn every SoC into "OpenIPC
    # publishes nothing for this part".
    soc = instructable_soc('TS3516EV400')

    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    ReleaseIndex.reset!

    submit(soc, 'nor8m')

    assert_no_match(/publishes no NOR firmware/, response.body)
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end

  # --- Ultimate on a 16MB chip ---

  # The edition menu forbade Ultimate on 16MB, and 16MB is the only chip
  # Ultimate is built for: every *_ultimate_defconfig upstream carries
  # BR2_OPENIPC_FLASH_SIZE="16". The rule hid the edition from the hardware it
  # targets. It cannot overflow either -- the build caps a NOR rootfs at 8192KB
  # and the 16MB layout gives it 10240KB.
  test 'the edition menu no longer withholds Ultimate from a 16MB chip' do
    soc = instructable_soc('TS3516EV500')

    with_release_index(*every_edition_for(soc)) do
      get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

      assert_response :success
      assert_match(/const layoutLimits = \{nor8m: \['lite'\]\};/, response.body)
    end
  end

  test 'Ultimate on 16MB is rendered as Ultimate, not quietly downgraded' do
    soc = instructable_soc('TS3516EV600')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m', firmware_version: 'ultimate')

      assert_match 'openipc-ts3516ev600-nor-ultimate-16mb.bin', response.body
      assert_no_match(/8MB Flash ROM can only be flashed/, response.body)
      assert_no_match(/does not fit an 8MB flash chip/, response.body)
    end
  end

  test '8MB still refuses Ultimate, so the size rule was narrowed and not dropped' do
    soc = instructable_soc('TS3516EV700')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor8m', firmware_version: 'ultimate')

      assert_match(/8MB Flash ROM can only be flashed/, response.body)
      assert_match 'openipc-ts3516ev700-nor-lite-8mb.bin', response.body
    end
  end

  # --- SigmaStar and Ingenic on 16MB ---

  # FlashLayout pinned these two vendors to the 8MB offsets whatever chip was
  # picked, so the page told a 16MB camera to `run uknor16m; run urnor16m` --
  # writing the rootfs to 0x350000..0xd50000 using the bootloader's own macros
  # -- and then erased from 0x750000, 733,184 bytes inside it. u-boot-msc313e,
  # u-boot-t20 and u-boot-t40 all define mtdpartsnor16m identically to the
  # Hisilicon and Goke ones, so there was never a reason to treat them apart.
  def soc_of(vendor_name)
    vendor = Vendor.create!(name: vendor_name)
    Soc.create!(vendor:, model: 'TS338Q', status: 'done', load_address: '0x82000000',
                uboot_filename: 'u-boot-ts338q-universal.bin',
                linux_filename: 'openipc.ts338q-nor-lite.tgz')
  end

  # FlashLayout pinned these two vendors to the 8MB offsets whatever chip was
  # picked, so the page told a 16MB camera to `run uknor16m; run urnor16m` --
  # writing the rootfs to 0x350000..0xd50000 using the bootloader's own macros
  # -- and then erased from 0x750000, 733,184 bytes inside it.
  def assert_sixteen_meg_offsets(vendor_name)
    soc = soc_of(vendor_name)

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m', firmware_version: 'ultimate')

      assert_match 'sf erase 0xD50000', response.body
      assert_no_match(/sf erase 0x750000/, response.body)
    end
  end

  # Those offsets are only right if the camera is running the 16MB mtdparts, and
  # every one of these bootloaders defaults to the 8MB one. A full-image flash
  # leaves the env erased, so that default is what boots. These two vendors used
  # to be the only ones not told to run setnor16m afterwards.
  def assert_told_to_remap_partitions(vendor_name)
    soc = soc_of(vendor_name)

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor16m')

      # The expert section further down emits `run setnor16m` for everybody and
      # always has, so matching the string alone proves nothing. What was
      # suppressed is the copy of it in the full-image section, which arrives
      # with the flashing_full.continue2 sentence in front of it.
      assert_match 'remap ROM partitioning according to your flash size', response.body
      assert_equal 2, response.body.scan('run setnor16m').size
    end
  end

  test 'a 16MB SigmaStar submission gets the 16MB layout like everyone else' do
    assert_sixteen_meg_offsets('SigmaStar')
  end

  test 'a 16MB Ingenic submission gets the 16MB layout like everyone else' do
    assert_sixteen_meg_offsets('Ingenic')
  end

  test 'a 16MB SigmaStar camera is told to run setnor16m after a full flash' do
    assert_told_to_remap_partitions('SigmaStar')
  end

  test 'a 16MB Ingenic camera is told to run setnor16m after a full flash' do
    assert_told_to_remap_partitions('Ingenic')
  end

  # --- the pages that send the visitor to the wiki instead ---

  # These two render their own template and no commands at all. The heading was
  # a hardcoded English "Attention!" on both, and the SigmaStar one carried a
  # stray `>` after the ERB tag that reached the page as literal text --
  # "...not included in this guide.>". Both are visible on openipc.org today.
  # Asserted in Russian on purpose. The English translation of this heading is
  # the word that was hardcoded, so an English assertion passes either way and
  # proves nothing.
  def assert_heading_is_translated
    assert_match %(<h2 class="mt-5 mb-3">Внимание!</h2>), response.body
  end

  # ?locale=ru, the way a visitor switches. This used to have to go through
  # I18n.with_locale because the parameter did nothing -- set_locale was
  # commented out of Multilang and every request rendered in English whatever it
  # asked for. Now that it is wired up, the test can use the real path.
  def submit_in_russian(soc, flash_type, firmware_version)
    submit(soc, flash_type, firmware_version:, locale: 'ru')
  end

  test 'the SigmaStar NAND page translates its heading and has no stray markup' do
    vendor = Vendor.create!(name: 'SigmaStar')
    soc = Soc.create!(vendor:, model: 'TS338Q', status: 'done', load_address: '0x22000000',
                      uboot_filename: 'u-boot-ts338q-universal.bin',
                      linux_filename: 'openipc.ts338q-nor-lite.tgz')

    with_release_index("openipc.#{soc.board}-nand-ultimate.tgz") do
      submit(soc, 'nand', firmware_version: 'ultimate')

      assert_match 'not included in this guide.</p>', response.body
      assert_no_match(/guide\.(&gt;|>)/, response.body)

      submit_in_russian(soc, 'nand', 'ultimate')
      assert_heading_is_translated
    end
  end

  test 'the HI3536 NVR page translates its heading too' do
    soc = instructable_soc('HI3536DV100')

    with_release_index(*every_edition_for(soc)) do
      submit_in_russian(soc, 'nor8m', 'lite')

      assert_heading_is_translated
    end
  end

  # --- the way out when the bootloader has no `&&` ---

  # guarded_flash joins the transfer to the erase with `&&` so a failed transfer
  # cannot reach the erase. `&&` is a hush feature, and the bootloader people
  # are running when they first follow this page is the stock vendor one -- the
  # reporter in OpenIPC/firmware#2299 got "the help entry for the first command"
  # back from theirs. Nothing is erased in that case, but nothing explains it
  # either, so the block that uses `&&` says what to do instead.
  test 'a block built with && carries the note about bootloaders that lack it' do
    soc = instructable_soc('TS3516EVD00')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor8m')

      assert_match '&& sf erase', response.body
      assert_match 'it does not understand <code>&amp;&amp;</code>', response.body
    end
  end

  test 'the note is translated, not another hardcoded English string' do
    soc = instructable_soc('TS3516EVD10')

    with_release_index(*every_edition_for(soc)) do
      submit(soc, 'nor8m', locale: 'ru')

      assert_match 'не понимает <code>&amp;&amp;</code>', response.body
    end
  end

  # --- the permanent link ---

  def permalink_for(**attrs)
    Camera.new(camera_mac_address: 'aa:bb:cc:dd:ee:ff', camera_ip_address: '10.0.0.5',
               server_ip_address: '10.0.0.1', network_interface: 'eth',
               sd_card_slot: 'nosd', **attrs).permalink
  end

  # Five of the seven fields, because five is what the form has. `net` and `sd`
  # are set on the camera by the link and then rendered nowhere: the selects for
  # them are commented out at show.html.erb:86-87, so the form posts neither and
  # `update` falls back to its own eth/nosd defaults whatever the link said.
  # Asserting them here would need `assigns`, which this app has no
  # rails-controller-testing for -- and it would assert a value that goes no
  # further than this request. That the link carries them at all is covered in
  # camera_test.rb; that they survive to the next page is not true today.
  def assert_form_reopened_on(chip, edition)
    assert_response :success
    assert_match 'value="aa:bb:cc:dd:ee:ff"', response.body
    assert_match 'value="10.0.0.5"', response.body
    assert_match 'value="10.0.0.1"', response.body
    assert_match %(<option selected="selected" value="#{chip}">), response.body
    assert_match %(<option selected="selected" value="#{edition}">), response.body
  end

  test 'a permanent link reopens the wizard on the configuration it names' do
    soc = instructable_soc('TS3516EV800')

    with_release_index(*every_edition_for(soc)) do
      # net and sd carry values the form has no field for, so this also covers
      # `show` accepting a link that names them rather than raising on one.
      get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}" \
          "#{permalink_for(flash_type: 'nor32m', firmware_version: 'ultimate',
                           network_interface: 'wifi', sd_card_slot: 'sd')}"

      assert_form_reopened_on('nor32m', 'ultimate')
    end
  end

  # permalink wrote `var` and this action read `ver`, so the edition was the one
  # field that did not survive the round trip -- a link for Ultimate reopened as
  # Lite. Every link anyone has shared was built by the old spelling, so `var`
  # stays readable rather than being swapped out.
  # The menu forbids Ultimate on an 8MB chip, but does it in JavaScript, after
  # this action has chosen which option opens selected. Reading `var` back made
  # that reachable: an old link for 8MB + Ultimate rendered the form on exactly
  # the combination `update` refuses.
  test 'an 8MB link does not open the form on an edition that does not fit' do
    soc = instructable_soc('TS3516EVA00')

    with_release_index(*every_edition_for(soc)) do
      get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}" \
          "#{permalink_for(flash_type: 'nor8m', firmware_version: 'ultimate')}"

      assert_response :success
      assert_match %(<option selected="selected" value="lite">), response.body
      assert_no_match(/<option selected="selected" value="ultimate">/, response.body)
    end
  end

  # The same carve-out enforce_eight_meg_limit makes. hi3516cv6xx and
  # hi3519dv500 are published as Ultimate and nothing else, so forcing Lite
  # would open the form on a build upstream has never produced.
  test 'an 8MB link keeps Ultimate where it is the only edition published' do
    soc = instructable_soc('TS3516EVB00')

    with_release_index("openipc.#{soc.board}-nor-ultimate.tgz") do
      get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}" \
          "#{permalink_for(flash_type: 'nor8m', firmware_version: 'ultimate')}"

      assert_response :success
      assert_match %(<option selected="selected" value="ultimate">), response.body
    end
  end

  # A permanent link names every field whether or not it has a value, so
  # `&ver=` is what a link built with no edition chosen looks like -- and the
  # menu produces exactly that, because allowedEditions falls back to '' for a
  # chip with nothing published. OpenIPC/firmware#1912 carries a real one.
  # Reading the empty string as an answer left the dropdown with nothing
  # selected at all.
  def assert_blank_edition_key_ignored(key, model)
    soc = instructable_soc(model)

    with_release_index(*every_edition_for(soc)) do
      get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}" \
          "?mac=aa-bb-cc-dd-ee-ff&cip=10.0.0.5&sip=10.0.0.1&net=both&rom=nor16m&#{key}=&sd=sd"

      assert_response :success
      assert_match %(<option selected="selected" value="lite">), response.body
    end
  end

  test 'an empty var= in a link is no edition rather than a blank one' do
    assert_blank_edition_key_ignored('var', 'TS3516EVC00')
  end

  test 'an empty ver= in a link is no edition rather than a blank one' do
    assert_blank_edition_key_ignored('ver', 'TS3516EVC10')
  end

  test 'a link shared before the spelling was fixed still carries its edition' do
    soc = instructable_soc('TS3516EV900')

    with_release_index(*every_edition_for(soc)) do
      get "/cameras/vendors/#{soc.vendor.to_param}/socs/#{soc.to_param}" \
          '?mac=aa-bb-cc-dd-ee-ff&cip=10.0.0.5&sip=10.0.0.1&net=eth&rom=nor32m&var=ultimate&sd=nosd'

      assert_form_reopened_on('nor32m', 'ultimate')
    end
  end
end
