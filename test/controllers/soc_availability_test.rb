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

  # --- the SoC page says why, per status: see the exhaustive test below ---

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

    badge = css_select('.nav-link.active .badge').first

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

  # --- the bootloader has to be published, not merely named ---

  # HI3536DV100 names u-boot-hi3536dv100-original.bin and upstream does not
  # publish it. `availability` said :firmware_only and the list row agreed,
  # while the page offered the wizard form -- which would have gone on to build
  # an image around a bootloader that does not exist.
  test 'a named but unpublished bootloader does not reach the wizard form' do
    soc = soc_for(model: 'AVGHOST', uboot: 'u-boot-avghost-original.bin',
                  linux: 'openipc.avghost-nor-lite.tgz')
    publish 'openipc.avghost-nor-lite.tgz'

    assert_equal :firmware_only, soc.availability

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

    assert_response :success
    assert_match(/keeps its own bootloader/i, response.body,
                 'the page offers the wizard for a bootloader nobody publishes')
    assert_no_match(/Please enter the camera configuration/i, response.body)
  end

  # A link we cannot honour is worse than no link: it reads as our download
  # being broken rather than as the chip being unsupported. MStar MSC313E is
  # the one live page that reached this branch with a filename set.
  test 'an unpublished bootloader is not offered as a download either' do
    soc = soc_for(model: 'AVGLINK', status: 'wip', uboot: 'u-boot-avglink-universal.bin')
    publish

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

    assert_response :success
    assert_no_match(%r{releases/download/latest/u-boot-avglink-universal\.bin}, response.body,
                    'the page links a bootloader that 404s on github.com')
  end

  test 'a published bootloader is still offered' do
    soc = soc_for(model: 'AVGREAL', status: 'wip', uboot: 'u-boot-avgreal.bin')
    publish 'u-boot-avgreal.bin'

    get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

    assert_response :success
    assert_match(%r{releases/download/latest/u-boot-avgreal\.bin}, response.body)
  end

  # --- every state, for every status ---

  # #189 asks for a test that walks the whole catalogue. The catalogue lives in
  # MySQL and the test database is loaded from db/schema.rb with no rows in it,
  # so there is nothing committed to walk -- that arrives with #161. What can be
  # guarded here is the state space the 126 rows land in: every status crossed
  # with every availability, which is what decides the wording. The count
  # against the live index is measured on the host and reported on the PR.
  WORDING = {
    wizard: /Please enter the camera configuration/i,
    firmware_only: /keeps its own bootloader/i,
    none: {
      'neq' => /no board to develop on/i,
      'rnd' => /research stage/i,
      'hlp' => /need someone to do the work/i,
      'wip' => /being worked on/i,
      'mvp' => /early .* build/i,
      'done' => /no OpenIPC build for/i
    }
  }.freeze

  test 'every status in every state renders the wording that state calls for' do
    rows = Soc::STATUS.keys.map(&:to_s).flat_map do |status|
      %i[wizard firmware_only none].map do |state|
        [state, status, soc_for(model: "AVX#{status.upcase}#{state.to_s.upcase.delete('_')}",
                                status: status,
                                uboot: state == :wizard ? "u-boot-avx#{status}#{state}.bin" : '',
                                linux: state == :none ? '' : "openipc.avx#{status}#{state}-nor-lite.tgz")]
      end
    end

    publish(*rows.reject { |state, _, _| state == :none }
                 .flat_map { |state, status, _| assets_for(state, status) })

    rows.each do |state, status, soc|
      assert_equal state, soc.availability, "#{status}/#{state} classified as #{soc.availability}"
      assert_equal(state != :none, soc.installable?, "#{status}/#{state} disagrees with installable?")

      get "/cameras/vendors/#{@vendor.to_param}/socs/#{soc.to_param}"

      assert_response :success
      expected = state == :none ? WORDING[:none].fetch(status) : WORDING[state]
      assert_match expected, response.body, "a #{status} page in state #{state} says the wrong thing"
      assert_no_match(/working hard/i, response.body, "a #{status}/#{state} page asks the reader to wait")
      assert_no_match(/translation missing/i, response.body)
    end
  end

  def assets_for(state, status)
    names = ["openipc.avx#{status}#{state}-nor-lite.tgz"]
    names << "u-boot-avx#{status}#{state}.bin" if state == :wizard
    names
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
