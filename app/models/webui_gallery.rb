# frozen_string_literal: true

# The list of WebUI screenshots on /web-interface, read from
# config/webui_gallery.yml.
#
# The list lives in a file rather than in the view because the view is not its
# only reader: tools/webui-gallery photographs a camera from the same manifest.
# When the WebUI gains or loses a page, one edit there changes both what gets
# photographed and what the page shows, so the gallery cannot end up describing
# a set of images nobody ever took.
class WebuiGallery
  MANIFEST = Rails.root.join('config/webui_gallery.yml')

  Screen = Struct.new(:slug, :caption, :cgi, :settle, :scene, keyword_init: true) do
    # The two files the tool produces for this screen. The tile is the
    # downscaled copy; the full-size one is only fetched when a visitor zooms.
    def tile
      "webui/#{slug}-thumb.webp"
    end

    def full
      "webui/#{slug}.webp"
    end

    def alt
      "#{caption} page of the OpenIPC web interface"
    end

    # Shows live video, so the tool has to cover the picture with a scene the
    # project is allowed to publish before it takes the shot.
    def scene?
      scene == true
    end
  end

  def self.screens
    @screens ||= YAML.load_file(MANIFEST)
                     .fetch('screens')
                     .map { |entry| Screen.new(**entry.symbolize_keys) }
                     .freeze
  end

  def self.slugs
    screens.map(&:slug)
  end
end
