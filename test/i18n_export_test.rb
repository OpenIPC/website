# frozen_string_literal: true

require 'test_helper'
require Rails.root.join('lib/i18n_export')

# The static frontend reads a JSON copy of the marketing catalogue, committed
# to the tree because the `build` job has Node and no Ruby (#159).
#
# A committed generated file is only honest while something checks it, and
# this is that something: it fails the moment config/locales/*.yml says one
# thing and frontend/apps/site/src/i18n/*.json says another.
class I18nExportTest < ActiveSupport::TestCase
  test 'the exported catalogue is what the locale files say today' do
    stale = I18n.available_locales.reject do |locale|
      path = I18nExport.path_for(locale)
      File.exist?(path) && File.read(path) == I18nExport.json_for(locale)
    end

    assert_empty stale,
                 "#{stale.join(', ')} out of date. config/locales/*.yml changed without the " \
                 'export being regenerated; run `bin/rails i18n:export` and commit the result.'
  end

  test 'every locale is exported' do
    assert_equal %i[en ru zh].sort, I18n.available_locales.sort,
                 'a locale was added; the frontend build needs to know about it'

    I18n.available_locales.each do |locale|
      assert_path_exists I18nExport.path_for(locale)
    end
  end

  test 'the export carries only what the frontend is allowed to read' do
    # The wizard, the Open Wall, devise and the validation messages stay in
    # Rails. Shipping them would put strings in the bundle no page can use and
    # would make every unrelated wizard edit dirty the export.
    #
    # INCLUDED's namespaces count as allowed, because that list is how a single
    # leaf is admitted without its whole namespace -- see the test below, which
    # holds the graft to exactly that leaf.
    grafted = I18nExport::INCLUDED.map(&:first)

    I18n.available_locales.each do |locale|
      extra = I18nExport.catalogue(locale).keys - I18nExport::NAMESPACES - grafted
      assert_empty extra, "#{locale} exports #{extra.join(', ')}, which is outside the allow-list"
    end
  end

  test 'a grafted key brings nothing but itself' do
    # #160 needed one string out of `snapshots`: the home page's wall mosaic
    # fills its empty tiles with the Open Wall's own "no signal" placeholder,
    # and the two halves of the site have to say it in the same words. The risk
    # the graft carries is that it quietly widens -- the namespace arrives
    # whole, and 27 wizard-adjacent strings the marketing pages cannot use ship
    # with it.
    I18n.available_locales.each do |locale|
      catalogue = I18nExport.catalogue(locale)

      I18nExport::INCLUDED.each do |path|
        leaf = path.inject(catalogue) { |node, key| node.is_a?(Hash) ? node[key] : nil }
        assert_kind_of String, leaf, "#{locale} did not graft #{path.join('.')}"
        assert_predicate leaf, :present?

        # Nothing beside it: the namespace holds exactly the path that was
        # asked for, with no siblings at any level.
        node = catalogue
        path[0..-2].each do |key|
          node = node[key]
          assert_equal 1, node.keys.size,
                       "#{locale}: #{key} carries #{node.keys.join(', ')}, not just the grafted leaf"
        end
      end
    end
  end

  test 'the admin area is not in the marketing catalogue' do
    I18n.available_locales.each do |locale|
      assert_not_includes I18nExport.catalogue(locale).fetch('pages', {}).keys, 'admin'
    end
  end

  test 'the export is JSON, with no symbols or procs left in it' do
    # lib/locale/plurals.rb stores a lambda under i18n.plural.rule, and the
    # backend stores every key as a symbol. Neither survives JSON, and a
    # silent nil in a catalogue is a blank page rather than an error.
    I18n.available_locales.each do |locale|
      round_tripped = JSON.parse(I18nExport.json_for(locale))
      assert_equal I18nExport.catalogue(locale), round_tripped,
                   "#{locale} does not survive a JSON round trip"
    end
  end

  test 'every English key the frontend can read is present' do
    # English is the fallback, so a key missing there is missing everywhere.
    # The frontend's t() throws on it at build time; this says so earlier, and
    # in the language of the locale files.
    en = flatten(I18nExport.catalogue(:en))
    blank = en.select { |_, v| v.nil? || (v.respond_to?(:empty?) && v.empty?) }

    assert_empty blank.keys, "empty English strings: #{blank.keys.take(5).join(', ')}"
  end

  private

  def flatten(node, prefix = nil, acc = {})
    case node
    when Hash then node.each { |k, v| flatten(v, [prefix, k].compact.join('.'), acc) }
    else acc[prefix] = node
    end
    acc
  end
end
