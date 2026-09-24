# frozen_string_literal: true

require 'yaml'
require 'json'

# The catalogue, from git-versioned YAML into the shape the prerendered
# hardware pages read (#161, #162).
#
# data/catalogue/*.yml is the source of truth -- one file per vendor, reviewed
# in a pull request. The Astro build cannot read YAML without another
# dependency, and more to the point it must not read a database: the pages are
# built in CI, where there is none. So the tree is baked into one JSON that the
# build imports, exactly as config/webui_gallery.yml is baked for the gallery.
#
# Two copies of anything can drift, so the copy is a function of the source and
# test/catalogue_export_test.rb fails the moment they disagree. Regenerate with
# `bin/rails catalogue:bake`.
#
# Only the fields the pages actually render are carried. The wizard's fields --
# uboot_filename, linux_filename, sdk -- stay out until #163 needs them, so a
# change to how firmware is assembled does not rebuild a marketing page.
module CatalogueExport
  SOC_FIELDS = %w[model family version urlname status load_address featured segment].freeze
  VENDOR_FIELDS = %w[name urlname full_name website_url].freeze

  OUT = 'frontend/apps/site/src/data/catalogue.json'
  DIR = 'data/catalogue'

  class << self
    def vendors(root: Rails.root)
      Dir[File.join(root, DIR, '*.yml')].sort.map { |file| vendor(YAML.safe_load_file(file)) }
                                        .sort_by { |v| v['name'] }
    end

    def json(root: Rails.root)
      "#{JSON.pretty_generate('vendors' => vendors(root: root))}\n"
    end

    def path(root: Rails.root)
      File.join(root, OUT)
    end

    def write(root: Rails.root)
      File.write(path(root: root), json(root: root))
    end

    private

    def vendor(data)
      data.slice(*VENDOR_FIELDS).merge(
        'socs' => data['socs'].map { |soc| soc.slice(*SOC_FIELDS) },
      )
    end
  end
end
