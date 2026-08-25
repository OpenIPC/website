require 'active_support/core_ext/integer/time'

Rails.application.configure do
  # Settings specified here will take precedence over those in config/application.rb.

  # In the development environment your application's code is reloaded any time
  # it changes. This slows down response time but is perfect for development
  # since you don't have to restart the web server when you make code changes.
  config.cache_classes = false

  # Do not eager load code on boot.
  config.eager_load = false

  # Show full error reports.
  config.consider_all_requests_local = true

  # Enable server timing
  config.server_timing = true

  config.hosts << "openipc.org"

  # Reach the dev server from other devices on the LAN -- a phone, or a second
  # machine -- rather than only from localhost.
  #
  # Rails 7 already ships IPAddr 0.0.0.0/0 in the development defaults, so bare
  # IP literals are allowed out of the box; only NAMED hosts need listing.
  #
  # The port suffix in the regex is not decoration. HostAuthorization matches
  # String and IPAddr entries against the Host header with the port stripped,
  # but Regexp entries against the header INCLUDING it. Written as
  # /.*\.local\z/ this rule would 403 every request to trainer-arch.local:3010.
  config.hosts << /.*\.local(:\d+)?\z/

  # Enable/disable caching. By default caching is disabled.
  # Run rails dev:cache to toggle caching.
  if Rails.root.join('tmp/caching-dev.txt').exist?
    config.action_controller.perform_caching = true
    config.action_controller.enable_fragment_cache_logging = true

    config.cache_store = :memory_store
    config.public_file_server.headers = {
      'Cache-Control' => "public, max-age=#{2.days.to_i}"
    }
  else
    config.action_controller.perform_caching = false

    config.cache_store = :null_store
  end

  # Store uploaded files on the local file system (see config/storage.yml for options).
  config.active_storage.service = :local

  # Don't care if the mailer can't send.
  config.action_mailer.raise_delivery_errors = false

  config.action_mailer.perform_caching = false

  # Print deprecation notices to the Rails logger.
  config.active_support.deprecation = :log

  # Raise exceptions for disallowed deprecations.
  config.active_support.disallowed_deprecation = :raise

  # Tell Active Support which deprecation messages to disallow.
  config.active_support.disallowed_deprecation_warnings = []

  # Raise an error on page load if there are pending migrations.
  config.active_record.migration_error = :page_load

  # Highlight code that triggered database queries in logs.
  config.active_record.verbose_query_logs = true

  # Suppress logger output for asset requests.
  config.assets.quiet = true

  # Behave like production: a key missing from ru or zh falls back to the
  # English text. Without this, development is the only environment that renders
  # a "translation missing" span, so a gap looks broken here and invisible there
  # -- or worse, the reverse, and a gap gets shipped because it looked fine.
  config.i18n.fallbacks = true

  # Raises error for missing translations.
  # config.i18n.raise_on_missing_translations = true

  # Annotate rendered view with file names.
  # config.action_view.annotate_rendered_view_with_filenames = true

  # Uncomment if you wish to allow Action Cable access from any origin.
  # config.action_cable.disable_request_forgery_protection = true

  config.web_console.allowed_ips = '192.168.1.0/24'
end
