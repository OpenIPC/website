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

  def submit(soc, flash_type, firmware_version: 'lite')
    put "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}",
        params: { camera: { flash_type:, firmware_version:,
                            network_interface: 'eth', sd_card_slot: 'nosd',
                            camera_ip_address: '192.168.1.10',
                            server_ip_address: '192.168.1.254',
                            camera_mac_address: '00:11:22:33:44:55' } }
    assert_response :success
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
end
