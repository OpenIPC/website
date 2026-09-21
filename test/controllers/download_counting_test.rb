# frozen_string_literal: true

require 'test_helper'

# One row per download, not one per HTTP request (#188).
#
# A browser or download manager fetching an 8-32MB image asks for it in chunks,
# and each chunk is its own request -- so the table counted requests. Unevenly,
# too: the 16MB ultimate images chunk hardest, so the chips people care most
# about were inflated most.
#
# Reconciled against the nginx log for 6-20 September 2026, 1,203 rows stood
# for 824 completed downloads. On 10 September, 99 range responses turned 55
# real downloads into 155 rows, which read as a spike and was not one.
#
# The failure is quiet in the direction that matters: the table is the input to
# the monthly memo (#184), and a number that is 46% high looks exactly like a
# number that is right.
class DownloadCountingTest < ActionDispatch::IntegrationTest
  setup do
    @vendor = Vendor.create!(name: 'Counting Probe Vendor')
    @soc = Soc.create!(vendor: @vendor, model: 'CNT1', status: 'done',
                       uboot_filename: 'u-boot-cnt1.bin',
                       linux_filename: 'openipc.cnt1-nor-lite.tgz')
    @cache = Dir.mktmpdir
    Firmware.cache_dir = @cache
    # A cached image, so the action sends a file rather than assembling one --
    # which is also the path most real downloads take.
    #
    # Exactly eight megabytes and world-readable, because Firmware#usable?
    # checks both: a NOR image is its flash size by construction, so a short
    # file is treated as a leftover and rebuilt. Sparse, so it costs no disk.
    name = Firmware.filename_for(soc_model: @soc.model_downcase, flash_type: 'nor',
                                 release: 'lite', size: 8)
    path = File.join(@cache, name)
    File.open(path, 'wb') { |f| f.truncate(8.megabytes) }
    File.chmod(0o644, path)
  end

  teardown do
    Firmware.cache_dir = nil
    FileUtils.remove_entry(@cache) if @cache
  end

  def download(range: nil)
    headers = range ? { 'Range' => range } : {}
    get "/cameras/vendors/#{@vendor.to_param}/socs/#{@soc.to_param}/download_full_image",
        params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }, headers: headers
  end

  test 'a plain request counts once' do
    assert_difference 'Download.count', 1 do
      download
    end
  end

  # Download managers that always send a range, starting at the beginning.
  # This is a download, and the only one it will send that starts at zero.
  test 'a range that starts at the beginning counts once' do
    assert_difference 'Download.count', 1 do
      download range: 'bytes=0-1048575'
    end
  end

  test 'an open-ended range from the beginning counts once' do
    assert_difference 'Download.count', 1 do
      download range: 'bytes=0-'
    end
  end

  # Every chunk after the first, and every resumed fetch. Same download.
  ['bytes=1048576-', 'bytes=1048576-2097151', 'bytes=33554432-'].each do |range|
    test "a continuation range #{range} does not count again" do
      assert_no_difference 'Download.count' do
        download range: range
      end
    end
  end

  # A header that arrives with whitespace is still a first chunk. Being strict
  # here fails in the opposite direction to the bug this replaces, and a worse
  # one: an uncounted download is indistinguishable from nobody downloading,
  # where an over-counted one at least looks suspicious.
  [' bytes=0-1023', 'bytes = 0-1023', "bytes=0-1023\t"].each do |range|
    test "a first chunk sent as #{range.inspect} still counts" do
      assert_difference 'Download.count', 1 do
        download range: range
      end
    end
  end

  # A range in a unit the file server does not implement is ignored and the
  # whole file is sent -- `Range: kbytes=0-4` comes back 200 with all 8,388,608
  # bytes. That is a download.
  #
  # The first version of this file asserted the opposite, which is how a bug
  # gets written down as a rule: the reader received the entire image and the
  # table recorded nothing.
  ['kbytes=0-4', 'items=0-4', 'bytes-and-more=0-4'].each do |range|
    test "a range in the unrecognised unit #{range.split('=').first} counts, because the whole file is sent" do
      assert_difference 'Download.count', 1 do
        download range: range
      end

      assert_equal 200, response.status
      assert_equal 8.megabytes, response.body.bytesize
    end
  end

  # A HEAD is a probe. Rails routes it to the same action and the reader gets
  # no body, so counting it records a download that did not happen. Eighteen
  # reached this path in fourteen days, against 1,871 GETs.
  test 'a HEAD probe does not count as a download' do
    assert_no_difference 'Download.count' do
      head "/cameras/vendors/#{@vendor.to_param}/socs/#{@soc.to_param}/download_full_image",
           params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }
    end

    assert_equal 0, response.body.bytesize
  end

  test 'a probe followed by the real download counts once' do
    assert_difference 'Download.count', 1 do
      head "/cameras/vendors/#{@vendor.to_param}/socs/#{@soc.to_param}/download_full_image",
           params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }
      download
    end
  end

  # `bytes=-500` asks for the last 500 bytes. Not a first chunk.
  test 'a suffix range does not count' do
    assert_no_difference 'Download.count' do
      download range: 'bytes=-500'
    end
  end

  # The shape of the real thing: one fetch, in chunks, is one download.
  test 'a chunked fetch of one image writes one row' do
    assert_difference 'Download.count', 1 do
      download range: 'bytes=0-1048575'
      8.times { |i| download range: "bytes=#{(i + 1) * 1_048_576}-" }
    end
  end

  # And two people each downloading once are two, however they ask.
  test 'two separate downloads are two rows' do
    assert_difference 'Download.count', 2 do
      download
      download range: 'bytes=0-'
    end
  end

  # The row still says what it always said. The rule changed which requests
  # write one, not what is written.
  test 'the row it does write is unchanged' do
    download

    row = Download.order(:id).last

    assert_equal @soc.model_downcase, row.soc_model
    assert_equal 'nor', row.flash_type
    assert_equal 'lite', row.release
    assert_equal 8, row.flash_size
    assert_operator row.bytes.to_i, :>, 0
  end
end
