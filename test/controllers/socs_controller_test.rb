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
end
