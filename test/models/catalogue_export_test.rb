# frozen_string_literal: true

require 'test_helper'
require 'yaml'

# The catalogue, in git (#161).
#
# data/catalogue/*.yml is what the prerendered hardware pages are built from
# (#162). It began as an export of the vendors and socs tables and is on its
# way to being the source of truth for both, so what it has to stay is
# well-formed and loadable: a file that cannot produce a valid Soc is a page
# that will not build, and the build happens in CI where this database does
# not exist.
#
# Deliberately not a comparison against the tables. The test database carries
# fixtures rather than the catalogue, so such a test would compare the files
# with an empty database and pass or fail for reasons that have nothing to do
# with the data. `bin/rails catalogue:diff` does that comparison where the real
# rows are -- on a host with the production database.
class CatalogueExportTest < ActiveSupport::TestCase
  EXPORTED = Soc.column_names - %w[id vendor_id created_at updated_at]

  def catalogue
    @catalogue ||= Dir[Rails.root.join('data/catalogue/*.yml')].sort.map do |file|
      [File.basename(file), YAML.safe_load_file(file)]
    end
  end

  test 'there is a catalogue at all' do
    assert_not_empty catalogue, 'data/catalogue is empty'
    assert_operator catalogue.sum { |_, v| v['socs'].size }, :>, 100,
                    'the catalogue lost most of its SoCs'
  end

  test 'a file is named after the vendor it holds' do
    catalogue.each do |name, vendor|
      assert_equal "#{vendor['urlname']}.yml", name,
                   'a vendor file is not named after its own urlname'
    end
  end

  test 'every SoC carries every column the pages read' do
    # A column added to the table and not to the export is the failure this
    # catches: the page renders, the new field is simply blank, and it reads as
    # a content problem rather than a missing export.
    catalogue.each do |name, vendor|
      vendor['socs'].each do |soc|
        assert_equal EXPORTED.sort, soc.keys.sort, "#{name}: #{soc['model']}"
      end
    end
  end

  test 'every SoC in the files is a valid record' do
    catalogue.each do |name, data|
      vendor = Vendor.new(data.slice('name', 'urlname', 'full_name', 'website_url', 'notes'))
      assert vendor.valid?, "#{name}: #{vendor.errors.full_messages.join(', ')}"

      data['socs'].each do |attrs|
        soc = Soc.new(attrs.merge('vendor' => vendor))
        assert soc.valid?, "#{name}: #{attrs['model']}: #{soc.errors.full_messages.join(', ')}"
      end
    end
  end

  test 'urlnames are unique across the whole catalogue, because they are URLs' do
    vendors = catalogue.map { |_, v| v['urlname'] }
    assert_equal vendors.uniq, vendors, 'two vendors share a urlname'

    socs = catalogue.flat_map { |_, v| v['socs'].map { |s| [v['urlname'], s['urlname']] } }
    assert_equal socs.uniq, socs, 'two SoCs of one vendor share a urlname'
  end

  test 'every status is one the pages know how to draw' do
    # The hardware page draws a stage icon per status and the wizard decides
    # what to offer from it; an unknown one renders a broken image.
    # The six the stage icons and the wizard's copy are written for; there is
    # no enum on the model, so the list lives with the check.
    known = %w[done mvp wip hlp neq rnd]
    catalogue.each do |name, vendor|
      vendor['socs'].each do |soc|
        assert_includes known, soc['status'], "#{name}: #{soc['model']}"
      end
    end
  end
end
