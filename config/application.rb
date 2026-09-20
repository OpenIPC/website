# frozen_string_literal: true

require_relative 'boot'

require 'rails/all'

# Require the gems listed in Gemfile, including any gems
# you've limited to :test, :development, or :production.
Bundler.require(*Rails.groups)

require_relative '../lib/asset_cache_control'

module Openipc
  class Application < Rails::Application
    # Not the originally generated version any more: the app was generated at 7.0
    # and this is raised deliberately, one minor at a time, with the suite green
    # at each step. Raising it is what opts in to the new framework defaults, so
    # it is the line that does the actual upgrading -- the gem version alone
    # changes almost nothing.
    config.load_defaults 8.1

    # Configuration for the application, engines, and railties goes here.
    #
    # These settings can be overridden in specific environments using the files
    # in config/environments, which are processed later.
    #
    # config.time_zone = "Central Time (US & Canada)"
    # config.eager_load_paths << Rails.root.join("extras")

    # Cache-Control for the fingerprinted assets. It has to sit outside
    # ActionDispatch::Static to see what Static served, but it cannot be
    # positioned against Static: Static is only in the stack when
    # public_file_server.enabled is true, and during the image build's
    # assets:precompile it is not, so insert_before ActionDispatch::Static
    # aborts the build with "No such middleware". Rack::Sendfile is
    # unconditional and sits immediately above Static, which is the same
    # position by a name that is always there.
    #
    # In lib/ and required above rather than autoloaded from app/: this line
    # runs while the application class is still being defined, long before
    # Zeitwerk could resolve the constant, and Rails 8.1 wants the class itself
    # -- a string reaches stack.rb and dies on `undefined method 'name'`.
    #
    # See lib/asset_cache_control.rb for why this is not the obvious
    # config.public_file_server.headers one-liner.
    config.middleware.insert_after Rack::Sendfile, AssetCacheControl
  end
end
