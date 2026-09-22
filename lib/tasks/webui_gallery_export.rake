# frozen_string_literal: true

namespace :webui_gallery do
  desc 'Write the WebUI screenshot manifest for the Astro build'
  task export: :environment do
    require Rails.root.join('lib/webui_gallery_export')
    path = WebuiGalleryExport.write
    puts "#{path}: #{WebuiGalleryExport.manifest.size} screen(s)"
  end
end
