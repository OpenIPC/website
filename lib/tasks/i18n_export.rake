# frozen_string_literal: true

namespace :i18n do
  desc 'Export the marketing catalogue to JSON for the Astro build (#159)'
  task export: :environment do
    require 'json'
    require 'fileutils'
    require Rails.root.join('lib/i18n_export')

    I18nExport.write_all.each { |path, n| puts "#{path}: #{n} string(s)" }
  end
end
