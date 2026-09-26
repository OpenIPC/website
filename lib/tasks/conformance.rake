# frozen_string_literal: true

namespace :conformance do
  desc "Write Rails' verdicts on upload content types and MAC spellings, for the conformance suite"
  task fixtures: :environment do
    require 'conformance_fixtures'
    ConformanceFixtures.write
    ConformanceFixtures.documents.each_key do |name|
      puts "wrote #{ConformanceFixtures.path(name).relative_path_from(Rails.root)}"
    end
  end
end
