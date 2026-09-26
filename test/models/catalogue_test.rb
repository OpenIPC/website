# frozen_string_literal: true

require 'test_helper'
require 'yaml'

# The catalogue, in git, and nowhere else (#161, #289).
#
# data/catalogue/*.yml is what the application reads and what the prerendered
# pages are built from. A file that cannot load is a process that cannot boot,
# which is the right failure -- but it should be found here, in CI, rather than
# by a container that will not start.
class CatalogueTest < ActiveSupport::TestCase
  def files
    @files ||= Dir[Rails.root.join('data/catalogue/*.yml')].sort.map do |file|
      [File.basename(file), YAML.safe_load_file(file)]
    end
  end

  def real
    @real ||= Catalogue.load
  end

  test 'the files load, and there is a catalogue at all' do
    assert_equal files.size, real.vendors.size
    assert_operator real.socs.size, :>, 100, 'the catalogue lost most of its SoCs'
  end

  test 'a file is named after the vendor it holds' do
    files.each do |name, vendor|
      assert_equal "#{vendor['urlname']}.yml", name, 'a vendor file is not named after its own urlname'
    end
  end

  # A field in the files and not on the model is silently dropped on load; one
  # on the model and not in the files renders blank. Either reads as a content
  # problem rather than a missing field, so the two lists are held equal.
  test 'every SoC carries exactly the fields the model reads' do
    files.each do |name, vendor|
      vendor['socs'].each do |soc|
        assert_equal Soc::ATTRIBUTES.sort, soc.keys.sort, "#{name}: #{soc['model']}"
      end
      assert_equal Vendor::ATTRIBUTES.sort, (vendor.keys - ['socs']).sort, name
    end
  end

  test 'every status is one the pages know how to draw' do
    # The hardware page draws a stage icon per status and the wizard decides
    # what to offer from it; an unknown one renders a broken image.
    real.socs.each do |soc|
      assert_includes Soc::STATUS.keys.map(&:to_s), soc.status, soc.urlname
    end
  end

  test 'lookups go by slug, and an unknown slug is a 404' do
    Catalogue.current = real
    soc = real.socs.first

    assert_equal soc, Soc.find(soc.urlname)
    assert_equal soc.vendor, Vendor.find(soc.vendor.urlname)
    assert_includes soc.vendor.socs, soc
    assert_raises(ActiveRecord::RecordNotFound) { Soc.find('no-such-chip') }
    assert_raises(ActiveRecord::RecordNotFound) { Vendor.find('1') }
    assert_nil Soc.find_by_param('')
  end

  # A slug is an address and a filename: it names a directory in the bundle and
  # a file the wizard export writes. One that walks out of either is refused at
  # load, and so is one that would make two chips answer at one address.
  def load_documents(*documents)
    Catalogue.new(documents.each_with_index.map { |data, i| ["v#{i}.yml", data] })
  end

  def chip(model, **extra)
    { 'model' => model, 'status' => 'done' }.merge(extra.transform_keys(&:to_s))
  end

  test 'a slug that is not safe does not load' do
    error = assert_raises(Catalogue::Invalid) do
      load_documents('name' => 'Acme', 'socs' => [chip('X1', urlname: '../../etc')])
    end
    assert_match(/v0\.yml.*not a safe slug/, error.message)

    assert_raises(Catalogue::Invalid) { load_documents('name' => 'Ac/me', 'socs' => []) }
  end

  test 'two chips at one address do not load' do
    assert_raises(Catalogue::Invalid) do
      load_documents({ 'name' => 'Acme', 'socs' => [chip('X1')] },
                     { 'name' => 'Other', 'socs' => [chip('x1')] })
    end
    assert_raises(Catalogue::Invalid) do
      load_documents('name' => 'Acme', 'socs' => [chip('X1'), chip('X1', urlname: 'x1-b')])
    end
  end

  test 'a segment that is not one does not load' do
    error = assert_raises(Catalogue::Invalid) do
      load_documents('name' => 'Acme', 'socs' => [chip('X1', segment: 'cctvv')])
    end
    assert_match(/Segment/, error.message)
  end

  # The records live as long as the process and the release index changes
  # hourly, so what a chip says about upstream must follow the index rather
  # than whatever it was the first time somebody asked.
  test 'availability follows a new release index' do
    root = Dir.mktmpdir
    ENV['RELEASE_INDEX_ROOT'] = root
    write_index = lambda do |assets|
      File.write(File.join(root, '.index.json'),
                 JSON.generate('generated_at' => Time.current.utc.iso8601, 'aliases' => {},
                               'assets' => assets.to_h { |n| [n, { 'size' => 1 }] }))
      stamp = Time.now + assets.size
      File.utime(stamp, stamp, File.join(root, '.index.json'))
    end

    vendor = Vendor.create!(name: 'Indexco')
    soc = Soc.create!(vendor: vendor, model: 'IX100', status: 'done',
                      uboot_filename: 'u-boot-ix100.bin', linux_filename: 'openipc.ix100-nor-lite.tgz')

    write_index.call([])
    assert_equal :none, soc.availability

    write_index.call(%w[openipc.ix100-nor-lite.tgz u-boot-ix100.bin])
    assert_equal :wizard, soc.availability, 'the answer was remembered from the older index'
  ensure
    ENV.delete('RELEASE_INDEX_ROOT')
    ReleaseIndex.reset!
    FileUtils.remove_entry(root) if root
  end
end
