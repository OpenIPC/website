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

    # The wall channel lives under /api/, not at the default /cable.
    #
    # Two reasons, both structural. #142 has the mirrors proxying only /api/,
    # /wall/i/ and /dl/firmware/ through to the origin, so a path under /api/
    # needs no edge rule when they become edge nodes in #167. And
    # deploy/static/reserved-paths already reserves /api/, so the static bundle
    # can never shadow it -- which is the failure mode #157 names as the most
    # likely way that seam goes wrong.
    config.action_cable.mount_path = '/api/v1/wall/cable'

    # Configuration for the application, engines, and railties goes here.
    #
    # These settings can be overridden in specific environments using the files
    # in config/environments, which are processed later.
    #
    # config.time_zone = "Central Time (US & Canada)"
    # config.eager_load_paths << Rails.root.join("extras")

    # Ruby, deliberately rather than by default (#236).
    #
    # The argument for structure.sql is that the Ruby dump cannot describe
    # everything a database holds; here it demonstrably can. Checked by
    # round-tripping the development database through db/schema.rb and
    # comparing `SHOW CREATE TABLE` for every table before and after: 196 lines
    # of DDL, identical, charset and collation included. Nothing is lost.
    #
    # What the Ruby dump buys is the thing #236 is about. A schema diff is the
    # one review in this repository where somebody is checking which column
    # moved and what a rollback will not undo -- deploy/DEV-VALIDATION.md and
    # CLAUDE.md both warn that rollback restores the image and never the schema
    # -- and structure.sql is markedly worse to read for that.
    #
    # Written out because a default is not a decision, and this one should
    # survive somebody's next look at it.
    config.active_record.schema_format = :ruby

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
