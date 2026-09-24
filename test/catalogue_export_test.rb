# frozen_string_literal: true

require 'test_helper'
require 'catalogue_export'

# The baked catalogue is a function of data/catalogue/*.yml (#161, #162).
#
# The frontend build imports the JSON rather than the YAML, so the two can
# drift: somebody edits sigmastar.yml in a pull request, the pages keep
# rendering last week's, and nothing says so. This is the something that says
# so.
class CatalogueExportTest < ActiveSupport::TestCase
  test 'the baked catalogue is what the YAML says today' do
    path = CatalogueExport.path

    assert_path_exists path, 'the catalogue has never been baked'
    assert_equal CatalogueExport.json, File.read(path),
                 'data/catalogue/*.yml changed without the bake being regenerated; ' \
                 'run `bin/rails catalogue:bake` and commit the result.'
  end

  test 'it carries every vendor and every SoC' do
    baked = JSON.parse(File.read(CatalogueExport.path))

    assert_equal Dir[Rails.root.join('data/catalogue/*.yml')].size, baked['vendors'].size
    assert_operator baked['vendors'].sum { |v| v['socs'].size }, :>, 100
  end

  test 'it carries the fields the pages render and not the wizard\'s' do
    soc = JSON.parse(File.read(CatalogueExport.path))['vendors'].first['socs'].first

    assert_equal CatalogueExport::SOC_FIELDS.sort, soc.keys.sort
    # The firmware filenames are the wizard's (#163), and a marketing page that
    # rebuilt when they changed would rebuild for no visible reason.
    assert_not_includes soc.keys, 'uboot_filename'
    assert_not_includes soc.keys, 'linux_filename'
  end

  test 'vendors come out in the order the tab strip shows them' do
    names = JSON.parse(File.read(CatalogueExport.path))['vendors'].map { |v| v['name'] }

    assert_equal names.sort, names, 'the vendor tabs would be out of order'
  end
end
