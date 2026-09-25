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
    # Every file the exporter writes, asked for the way it writes them. Named
    # one by one this missed the wall's dictionaries entirely the day they were
    # added (#282): the exporter wrote three more files and the test went on
    # checking the same two.
    stale = I18n.available_locales.flat_map do |locale|
      I18nExport.files_for(locale, Rails.root).map { |path, json, _tree| [path, json] }
    end.reject { |path, json| File.exist?(path) && File.read(path) == json }
      .map { |path, _| File.basename(path) }

    assert_empty stale,
                 "#{stale.join(', ')} out of date. config/locales/*.yml changed without the " \
                 'export being regenerated; run `bin/rails i18n:export` and commit the result.'
  end

  test 'every locale is exported' do
    assert_equal %i[en ru zh].sort, I18n.available_locales.sort,
                 'a locale was added; the frontend build needs to know about it'

    I18n.available_locales.each do |locale|
      I18nExport.files_for(locale, Rails.root).each { |path, _json, _tree| assert_path_exists path }
    end

    # And every island the exporter knows has a dictionary in every locale: a
    # new one added to ISLANDS without an export is a missing import at build
    # time, which is a stack trace rather than a sentence.
    I18nExport::ISLANDS.each_key do |name|
      I18n.available_locales.each do |locale|
        assert_path_exists I18nExport.island_path_for(name, locale)
      end
    end
  end

  test 'the wizard dictionary carries the wizard and nothing else' do
    # The island imports this file, so every string in it is downloaded by
    # anyone who opens a SoC page. It is held to exactly WIZARD's subtrees for
    # the same reason the catalogue is held to its allow-list: a namespace that
    # arrives whole puts strings nobody renders in front of every translator,
    # and here it also puts them on the wire.
    I18n.available_locales.each do |locale|
      catalogue = I18nExport.wizard_catalogue(locale)

      I18nExport::WIZARD.each do |path|
        node = path.inject(catalogue) { |parent, key| parent.is_a?(Hash) ? parent[key] : nil }

        assert_not_nil node, "#{locale} did not export #{path.join('.')}"
      end

      walk = lambda do |node, prefix|
        return unless node.is_a?(Hash)
        return if I18nExport::WIZARD.any? { |path| path.size <= prefix.size && path == prefix[0, path.size] }

        node.each_key do |key|
          here = prefix + [key]
          assert I18nExport::WIZARD.any? { |path| path[0, here.size] == here },
                 "#{locale}: wizard.json carries #{here.join('.')} and nothing asked for it"
          walk.call(node[key], here)
        end
      end
      walk.call(catalogue, [])
    end
  end

  test 'the two dictionaries overlap only where both halves render the string' do
    # Both files come out of config/locales in one pass, so a string in both
    # cannot disagree with itself. What an overlap costs is bytes, and these
    # six are the ones worth their weight: the layout resolves the page title
    # at build time while the island renders the rest of that subtree at
    # runtime, the island draws the breadcrumb every other page draws from
    # `nav`, and the "ask about this hardware" list reads the community page's
    # own description of each room rather than keeping a second wording.
    #
    # The assertion is the list, so widening it is a decision somebody makes
    # rather than something that happens.
    expected = %w[
      cameras.socs.show.title
      nav.home nav.vendors
      pages.community.channel_en pages.community.channel_fpv pages.community.channel_ru
    ].sort

    I18n.available_locales.each do |locale|
      both = flatten(I18nExport.catalogue(locale)).keys & flatten(I18nExport.wizard_catalogue(locale)).keys

      assert_equal expected, both.sort, "#{locale} exports #{both.join(', ')} in both files"
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

  test 'a graft brings nothing but itself' do
    # #160 needed one string out of `snapshots`: the home page's wall mosaic
    # fills its empty tiles with the Open Wall's own "no signal" placeholder,
    # and the two halves of the site have to say it in the same words. #162
    # needed two subtrees out of `cameras`: the catalogue's table and row, but
    # not the wizard's 27 strings, which no page in the bundle can use.
    #
    # The risk a graft carries is that it quietly widens -- the namespace
    # arrives whole, and strings nobody renders go in front of translators. So
    # this asserts the shape rather than the depth: every key the graft brings,
    # at every level, is on a path that was asked for.
    I18n.available_locales.each do |locale|
      catalogue = I18nExport.catalogue(locale)

      I18nExport::INCLUDED.each do |path|
        node = path.inject(catalogue) { |parent, key| parent.is_a?(Hash) ? parent[key] : nil }

        assert_not_nil node, "#{locale} did not graft #{path.join('.')}"
        assert node.is_a?(String) ? node.present? : node.any?,
               "#{locale} grafted #{path.join('.')} empty"
      end

      # Walk each grafted namespace and refuse a key no INCLUDED path names.
      I18nExport::INCLUDED.map(&:first).uniq.each do |root|
        next unless catalogue.key?(root)

        wanted = I18nExport::INCLUDED.select { |path| path.first == root }
        walk = lambda do |node, prefix|
          return unless node.is_a?(Hash)
          # At or past the end of a grafted path, everything below belongs.
          return if wanted.any? { |path| path.size <= prefix.size && path == prefix[0, path.size] }

          node.each_key do |key|
            here = prefix + [key]
            assert wanted.any? { |path| path[0, here.size] == here },
                   "#{locale}: #{here.join('.')} came with the graft and nothing asked for it"
            walk.call(node[key], here)
          end
        end
        walk.call(catalogue[root], [root])
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
      assert_equal I18nExport.catalogue(locale), JSON.parse(I18nExport.json_for(locale)),
                   "#{locale} does not survive a JSON round trip"
      assert_equal I18nExport.wizard_catalogue(locale), JSON.parse(I18nExport.wizard_json_for(locale)),
                   "#{locale}'s wizard dictionary does not survive a JSON round trip"
    end
  end

  test 'every English key the frontend can read is present' do
    # English is the fallback, so a key missing there is missing everywhere.
    # The frontend's t() throws on it at build time; this says so earlier, and
    # in the language of the locale files.
    en = flatten(I18nExport.catalogue(:en)).merge(flatten(I18nExport.wizard_catalogue(:en)))
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
