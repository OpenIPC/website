# frozen_string_literal: true

require 'test_helper'

# What a visitor can do with each chip, said on the page (#189).
#
# The catalogue lists 126 SoCs and the lists said one of two things: "Generate
# guide", or "There is no ready solution yet" for everything else. That second
# string covered a chip with published firmware and no bootloader -- which is
# installable, through the camera's own bootloader -- and a chip nobody has
# hardware for, and read the same either way.
#
# The SoC page was worse: every unsupported chip got "we are working hard to
# release OpenIPC firmware". For the 25 `neq` chips the truth is that we have
# the SDK and no board, and for the `hlp` ones that we have the board and need
# a developer. Both are things a reader could act on, and the copy asked them
# to wait instead.
#
# Ten of the fourteen vendor tabs contain no installable chip at all, and they
# took 55% of vendor-page views in the 19-20 September sample.
class SocAvailabilityTest < ActionDispatch::IntegrationTest
  setup do
    @vendor = Vendor.create!(name: 'Availability Probe Vendor')
    @root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = @root
  end

  teardown do
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(@root) if @root
  end

  # The index decides two of the three states, so it is stubbed rather than
  # mocked: these are the real predicates reading a real file.
  def publish(*assets)
    File.write(File.join(@root, '.index.json'),
               JSON.generate('generated_at' => Time.current.utc.iso8601, 'aliases' => {},
                             'assets' => assets.to_h { |n| [n, { 'size' => 1 }] }))
    ReleaseIndex.reset!
  end

  def soc_for(model:, status: 'done', uboot: '', linux: '')
    Soc.create!(vendor: @vendor, model:, status:, load_address: '0x80000000',
                uboot_filename: uboot, linux_filename: linux)
  end

  # --- the three states ---

  test 'a chip with both published is a wizard chip' do
    soc = soc_for(model: 'AVWIZ', uboot: 'u-boot-avwiz.bin', linux: 'openipc.avwiz-nor-lite.tgz')
    publish 'u-boot-avwiz.bin', 'openipc.avwiz-nor-lite.tgz'

    assert_equal :wizard, soc.availability
  end

  test 'firmware without a bootloader is its own state, not "nothing"' do
    soc = soc_for(model: 'AVFW', uboot: '', linux: 'openipc.avfw-nor-lite.tgz')
    publish 'openipc.avfw-nor-lite.tgz'

    assert_equal :firmware_only, soc.availability
    assert_predicate soc, :installable?, 'a bundle exists; this chip can be installed'
  end

  test 'nothing published is nothing' do
    soc = soc_for(model: 'AVNONE', status: 'neq')
    publish

    assert_equal :none, soc.availability
    assert_not_predicate soc, :installable?
  end

  # --- the list says the state in words ---

  test 'the list distinguishes the three states' do
    soc_for(model: 'AVLWIZ', uboot: 'u-boot-avlwiz.bin', linux: 'openipc.avlwiz-nor-lite.tgz')
    soc_for(model: 'AVLFW', linux: 'openipc.avlfw-nor-lite.tgz')
    soc_for(model: 'AVLNEQ', status: 'neq')
    publish 'u-boot-avlwiz.bin', 'openipc.avlwiz-nor-lite.tgz', 'openipc.avlfw-nor-lite.tgz'

    get "/cameras/vendors/#{@vendor.to_param}"

    assert_response :success
    assert_select '#avlwiz', text: /Generate an installation guide/
    assert_select '#avlfw', text: /Firmware only/
    assert_select '#avlneq', text: /no board to develop on/
    assert_no_match(/no ready solution yet/i, response.body)
  end

  # --- the SoC page says why, per status ---

  # The one this issue exists for. A reader on a `neq` page was told to wait;
  # what they could actually do is send a board.
  { 'neq' => /no board to develop on/i,
    'hlp' => /need someone to do the work/i,
    'rnd' => /research stage/i,
    'wip' => /being worked on/i,
    'mvp' => /early .* build/i }.each do |status, wording|
    test "a #{status} page says what is actually in the way" do
      soc = soc_for(model: "AVS#{status.upcase}", status: status)
      publish

      get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

      assert_response :success
      assert_match wording, response.body
      assert_no_match(/working hard/i, response.body,
                      "a #{status} page still asks the reader to wait")
    end
  end

  test 'a firmware-only page says how to flash it' do
    soc = soc_for(model: 'AVSFW', linux: 'openipc.avsfw-nor-lite.tgz')
    publish 'openipc.avsfw-nor-lite.tgz'

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

    assert_response :success
    # The bundle was linked with no path to using it. What matters is that the
    # page now says where the bootloader comes from and links the guide -- not
    # any particular phrase, so this asserts the link and the fact rather than
    # the wording, which is translated three ways.
    assert_match(/keeps its own bootloader/i, response.body)
    assert_match(%r{wiki/blob/master/en/installation\.md}, response.body,
                 'the bundle is linked with no path to using it')
  end

  # --- the vendor tab carries the count ---

  test 'the tab count is the number of installable chips' do
    soc_for(model: 'AVT1', uboot: 'u-boot-avt1.bin', linux: 'openipc.avt1-nor-lite.tgz')
    soc_for(model: 'AVT2', linux: 'openipc.avt2-nor-lite.tgz')
    soc_for(model: 'AVT3', status: 'neq')
    publish 'u-boot-avt1.bin', 'openipc.avt1-nor-lite.tgz', 'openipc.avt2-nor-lite.tgz'

    get "/cameras/vendors/#{@vendor.to_param}"

    badge = css_select(".nav-link.active .badge").first

    assert badge, 'the active vendor tab carries no count'
    assert_equal '2', badge.text.strip, 'the wizard chip and the firmware-only chip are both installable'
  end

  # Search traffic lands on these pages and they stay. They should say so once,
  # at the top, rather than let the reader work it out one row at a time.
  test 'a vendor with nothing installable says so before the list' do
    soc_for(model: 'AVE1', status: 'neq')
    soc_for(model: 'AVE2', status: 'rnd')
    publish

    get "/cameras/vendors/#{@vendor.to_param}"

    assert_response :success
    assert_select '.alert-warning', 1
    assert_match(/None of the 2 .* chips can be installed yet/, response.body)
  end

  test 'a vendor with something installable does not say that' do
    soc_for(model: 'AVN1', uboot: 'u-boot-avn1.bin', linux: 'openipc.avn1-nor-lite.tgz')
    publish 'u-boot-avn1.bin', 'openipc.avn1-nor-lite.tgz'

    get "/cameras/vendors/#{@vendor.to_param}"

    assert_select '.alert-warning', 0
  end

  # --- three locales ---

  test 'the states are translated, not left in English' do
    soc = soc_for(model: 'AVRU', status: 'neq')
    publish

    %w[/ru /zh].each do |prefix|
      get "#{prefix}/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

      assert_response :success
      assert_no_match(/translation missing/i, response.body)
      assert_no_match(/no board to develop on/, response.body, "#{prefix} shows the English sentence")
    end
  end
end
