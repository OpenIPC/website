# frozen_string_literal: true

# data/catalogue/*.yml is the catalogue (#161, #289): the application reads it
# through Catalogue, and the prerendered pages through the JSON this bakes,
# because the Astro build cannot read YAML without another dependency.
namespace :catalogue do
  desc 'Bake data/catalogue into the JSON the frontend build reads'
  task bake: :environment do
    require 'catalogue_export'
    CatalogueExport.write
    puts "wrote #{CatalogueExport::OUT}"
  end
end
