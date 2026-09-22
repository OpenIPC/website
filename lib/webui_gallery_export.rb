# frozen_string_literal: true

# The WebUI screenshot manifest, as the static frontend needs it (#160).
#
# config/webui_gallery.yml stays the source of truth: tools/webui-gallery reads
# it to know which pages of a camera to photograph, and it is the reason the
# page and the photographs cannot describe different sets of screens. The Astro
# build runs under Node, where the YAML and the Screen struct do not exist, so
# this writes the same list as JSON.
#
# Same arrangement as I18nExport and for the same two reasons: the bundle is
# built in a job that has Node and no Ruby, and a generated file in the tree is
# reviewable in a diff while a stale one is a red test.
#
# Regenerate with `bin/rails webui_gallery:export`.
module WebuiGalleryExport
  OUT = 'frontend/apps/site/src/data/webui-gallery.json'

  class << self
    # Only what the page renders. `cgi`, `settle` and `scene` are instructions
    # to the camera photographer and mean nothing to a browser; shipping them
    # would put the shape of our lab rig in a public bundle for no reason.
    def manifest
      WebuiGallery.screens.map do |screen|
        {
          'slug' => screen.slug,
          'caption' => screen.caption,
          'alt' => screen.alt,
        }
      end
    end

    def json
      "#{JSON.pretty_generate(manifest)}\n"
    end

    def path(root: Rails.root)
      File.join(root, OUT)
    end

    def write(root: Rails.root)
      FileUtils.mkdir_p(File.dirname(path(root: root)))
      File.write(path(root: root), json)
      path(root: root)
    end
  end
end
