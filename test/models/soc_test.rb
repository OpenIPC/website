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
    assert_equal '/srv/github-releases/openipc.t23-nor-lite.tgz',
                 soc.linux_file('lite', 'nor')
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

    assert_equal '/srv/github-releases/openipc.ts3519dv500-nor-lite.tgz',
                 soc.linux_file('lite', 'nor')
    assert_equal '/srv/github-releases/openipc.ts3519dv500-nor-ultimate.tgz',
                 soc.linux_file('ultimate', 'nor')
    assert_equal '/srv/github-releases/openipc.ts3519dv500-nand-ultimate.tgz',
                 soc.linux_file('ultimate', 'nand')
  end
end
