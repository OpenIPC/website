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

      deep_sort(picked)
    end

    # Sorted and newline-terminated, so the file is a function of the YAML
    # alone. Without that the staleness test would fail on hash ordering.
    def json_for(locale)
      "#{JSON.pretty_generate(catalogue(locale))}\n"
    end

    def path_for(locale, root: Rails.root)
      File.join(root, OUT_DIR, "#{locale}.json")
    end

    def write_all(root: Rails.root)
      FileUtils.mkdir_p(File.join(root, OUT_DIR))

      I18n.available_locales.map do |locale|
        path = path_for(locale, root: root)
        File.write(path, json_for(locale))
        [path, count_leaves(catalogue(locale))]
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
