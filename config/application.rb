# frozen_string_literal: true

require_relative 'boot'

require 'rails/all'

# Require the gems listed in Gemfile, including any gems
# you've limited to :test, :development, or :production.
Bundler.require(*Rails.groups)

module Openipc
  class Application < Rails::Application
    # Not the originally generated version any more: the app was generated at 7.0
    # and this is raised deliberately, one minor at a time, with the suite green
    # at each step. Raising it is what opts in to the new framework defaults, so
    # it is the line that does the actual upgrading -- the gem version alone
    # changes almost nothing.
    config.load_defaults 7.2

    # Configuration for the application, engines, and railties goes here.
    #
    # These settings can be overridden in specific environments using the files
    # in config/environments, which are processed later.
    #
    # config.time_zone = "Central Time (US & Canada)"
    # config.eager_load_paths << Rails.root.join("extras")
  end
end
