# frozen_string_literal: true

# The marketing catalogue, as the static frontend needs it (#159).
#
# config/locales/*.yml stays the source of truth. i18n-tasks keeps working on
# it and the Rails views keep reading it; this writes a JSON copy the Astro
# build can import, because Astro runs under Node where the YAML, the
# fallbacks and the namespace merge do not exist.
#
# The export is COMMITTED and test/i18n_export_test.rb fails when it drifts
# from what this would write now. Two reasons:
#
#   * the static bundle is built in the `build` job, which has Node and no
#     Ruby, so the export cannot be produced there;
#   * a generated file in the tree is reviewable in a diff, and a stale one is
#     a red test rather than a page quietly serving last week's wording.
#
# Regenerate with `bin/rails i18n:export`.
module I18nExport
  # What the frontend is allowed to read.
  #
  # An allow-list rather than the whole catalogue: `cameras`, `firmware`,
  # `flash_chip`, `flash_layout`, `net_iface`, `out`, `sd_card` and
  # `snapshots` belong to the installation wizard and the Open Wall, which
  # stay in Rails; `devise`, `activerecord` and `activemodel` are forms and
  # validations no static page renders. Exporting them would ship strings the
  # frontend cannot use, and would make every unrelated wizard edit dirty this
  # file.
  #
  # When #160 finds a page reaching for something outside this list, the list
  # grows on purpose, in a commit that says which page needed it.
  NAMESPACES = %w[button footer go nav site str support title pages].freeze

  # The admin area is not part of the marketing surface.
  EXCLUDED = [%w[pages admin]].freeze

  # Single keys from outside the namespaces above, grafted in by path.
  #
  # `snapshots` as a whole stays in Rails with the Open Wall, and #160 does not
  # move it. But the home page's wall mosaic fills its empty tiles with the Open
  # Wall's own "no signal" placeholder, and the two halves of the site must say
  # it in the same words -- a second key would drift the moment either is
  # retranslated. Grafting the one leaf is cheaper than admitting 27 wizard-
  # adjacent strings the marketing pages cannot use.
  INCLUDED = [
    %w[snapshots index no_signal],

    # The hardware catalogue's own strings (#162). `cameras` as a whole is not
    # a namespace here: most of it belongs to the wizard, which ships as its
    # own dictionary below, and admitting the lot would put 27 strings the
    # catalogue pages cannot use in front of every translator. These two
    # subtrees are the table and the row.
    %w[cameras socs index],
    %w[cameras socs soc],

    # The wizard page's <title>, which the layout renders at build time from
    # lib/page-paths.ts. The rest of that subtree is the wizard's own and
    # arrives with WIZARD instead.
    %w[cameras socs show title],
  ].freeze

  # The installation wizard's strings, exported separately (#164).
  #
  # The wizard is an island: what it renders depends on a query string and on a
  # release index fetched at runtime, so its copy cannot be resolved into the
  # HTML at build time the way every other page's is. It has to reach the
  # browser as data.
  #
  # Which is why it is not simply added to the list above. These seven subtrees
  # are 14 KB, and props are serialised into the page that carries the island:
  # passed that way they would add 14 KB to each of 378 prerendered wizard
  # pages -- 5 MB of identical JSON -- where a module the island imports is one
  # chunk, fetched once and cached for the whole catalogue.
  #
  # `nav` and `pages.community` arrive by the leaf. The island renders the
  # breadcrumb, and the "ask about this hardware" list reads the community
  # page's own description of each room rather than keeping a second copy.
  WIZARD = [
    %w[cameras socs show],
    %w[cameras socs update],
    %w[cameras socs warnings],
    %w[cameras socs sigmastar_nand_is_weird],
    %w[cameras socs hi3536dv100_is_weird],
    %w[firmware],
    %w[flash_chip],
    %w[flash_layout],
    %w[activemodel attributes camera],
    # bootstrap_form reads the help line under a field from here, and the two
    # menus that have one are the wizard's.
    %w[activerecord help camera],
    %w[nav home],
    %w[nav vendors],
    %w[pages community channel_en],
    %w[pages community channel_fpv],
    %w[pages community channel_ru],
  ].freeze

  OUT_DIR = 'frontend/apps/site/src/i18n'

  class << self
    # The backend's merged view -- what a Rails view sees, with every
    # `pages.*.yml` folded into the same tree as `<locale>.yml`.
    def catalogue(locale)
      I18n.backend.send(:init_translations) unless I18n.backend.initialized?
      all = I18n.backend.send(:translations)[locale.to_sym] || {}

      # Stringify first: the backend's hashes are frozen, so pruning has to
      # happen on the copy rather than on what every Rails view reads.
      picked = stringify(all.slice(*NAMESPACES.map(&:to_sym)))

      EXCLUDED.each do |path|
        parent = path[0..-2].inject(picked) { |node, key| node.is_a?(Hash) ? node[key] : nil }
        parent.delete(path.last) if parent.is_a?(Hash)
      end

      graft(picked, all, INCLUDED)

      deep_sort(picked)
    end

    # The wizard's own dictionary -- WIZARD's subtrees and nothing else.
    def wizard_catalogue(locale)
      I18n.backend.send(:init_translations) unless I18n.backend.initialized?
      all = I18n.backend.send(:translations)[locale.to_sym] || {}

      deep_sort(graft({}, all, WIZARD))
    end

    # Copy the named subtrees out of the backend's view and into `picked`.
    def graft(picked, all, paths)
      paths.each do |path|
        value = path.inject(all) { |node, key| node.is_a?(Hash) ? node[key.to_sym] : nil }
        # A key that has fallen out of the YAML is not grafted as nil: the
        # frontend treats a missing English key as a build failure, and a null
        # would defeat that by looking present.
        next if value.nil?

        parent = path[0..-2].inject(picked) { |node, key| node[key] ||= {} }
        parent[path.last] = stringify(value)
      end

      picked
    end

    # Sorted and newline-terminated, so the file is a function of the YAML
    # alone. Without that the staleness test would fail on hash ordering.
    def json_for(locale)
      "#{JSON.pretty_generate(catalogue(locale))}\n"
    end

    def wizard_json_for(locale)
      "#{JSON.pretty_generate(wizard_catalogue(locale))}\n"
    end

    def path_for(locale, root: Rails.root)
      File.join(root, OUT_DIR, "#{locale}.json")
    end

    def wizard_path_for(locale, root: Rails.root)
      File.join(root, OUT_DIR, "wizard.#{locale}.json")
    end

    def write_all(root: Rails.root)
      FileUtils.mkdir_p(File.join(root, OUT_DIR))

      I18n.available_locales.flat_map do |locale|
        [[path_for(locale, root: root), json_for(locale), catalogue(locale)],
         [wizard_path_for(locale, root: root), wizard_json_for(locale), wizard_catalogue(locale)]]
          .map do |path, json, tree|
            File.write(path, json)
            [path, count_leaves(tree)]
          end
      end
    end

    def count_leaves(node)
      case node
      when Hash  then node.values.sum { |v| count_leaves(v) }
      when Array then node.sum { |v| count_leaves(v) }
      else 1
      end
    end

    private

    # Symbols are how the backend stores keys and are not JSON. Procs are how
    # a pluralization rule is stored -- lib/locale/plurals.rb puts a lambda
    # under `i18n.plural.rule` -- and are not exportable either; the frontend
    # uses Intl.PluralRules, which knows the same CLDR categories.
    def stringify(node)
      case node
      when Hash
        node.each_with_object({}) do |(k, v), acc|
          next if v.is_a?(Proc)

          value = stringify(v)
          acc[k.to_s] = value unless value.nil?
        end
      when Array  then node.map { |v| stringify(v) }
      when Symbol then node.to_s
      when Proc   then nil
      else node
      end
    end

    def deep_sort(node)
      return node unless node.is_a?(Hash)

      node.keys.sort.each_with_object({}) { |k, acc| acc[k] = deep_sort(node[k]) }
    end
  end
end
