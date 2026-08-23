require 'active_support/core_ext/integer/time'

Rails.application.configure do
  # Settings specified here will take precedence over those in config/application.rb.

  # Code is not reloaded between requests.
  config.cache_classes = true

  # Eager load code on boot. This eager loads most of Rails and
  # your application in memory, allowing both threaded web servers
  # and those relying on copy on write to perform better.
  # Rake tasks automatically ignore this option for performance.
  config.eager_load = true

  # Full error reports are disabled and caching is turned on.
  config.consider_all_requests_local = false
  config.action_controller.perform_caching = true

  # Ensures that a master key has been made available in either ENV['RAILS_MASTER_KEY']
  # or in config/master.key. This key is used to decrypt credentials (and other encrypted files).
  #
  # SECRET_KEY_BASE_DUMMY lets `assets:precompile` run during the Docker build
  # without the real key, so RAILS_MASTER_KEY never has to be a CI secret. This
  # is the Rails 7.1 convention, backported by hand -- 7.0 has no such escape
  # hatch. It is only ever set in the image build, never at runtime.
  config.require_master_key = ENV['SECRET_KEY_BASE_DUMMY'].blank?

  # Disable serving static files from the `/public` folder by default since
  # Apache or NGINX already handles this.
  config.public_file_server.enabled = ENV['RAILS_SERVE_STATIC_FILES'].present?

  # Compress CSS using a preprocessor.
  # config.assets.css_compressor = :sass

  # Do not fallback to assets pipeline if a precompiled asset is missed.
  # Assets are precompiled into the image; compiling in-process is what made
  # the old bare-metal Puma sit at 2.4 GB RSS.
  config.assets.compile = false

  # Enable serving of images, stylesheets, and JavaScripts from an asset server.
  # config.asset_host = 'http://assets.example.com'

  # Specifies the header that your server uses for sending files.
  # config.action_dispatch.x_sendfile_header = 'X-Sendfile' # for Apache
  # config.action_dispatch.x_sendfile_header = 'X-Accel-Redirect' # for NGINX

  # Store uploaded files on the local file system (see config/storage.yml for options).
  config.active_storage.service = :local

  # Mount Action Cable outside main process or domain.
  # config.action_cable.mount_path = nil
  # config.action_cable.url = 'wss://example.com/cable'
  # config.action_cable.allowed_request_origins = [ 'http://example.com', /http:\/\/example.*/ ]

  # Force all access to the app over SSL, use Strict-Transport-Security, and use secure cookies.
  config.force_ssl = true

  # ...but not the healthcheck: the container probe speaks plain HTTP to the
  # published port and would otherwise get a 301 instead of a 200.
  config.ssl_options = {
    redirect: { exclude: ->(request) { request.path == '/up' } }
  }

  # Include generic and useful information about system operation, but avoid logging too much
  # information to avoid inadvertent exposure of personally identifiable information (PII).
  config.log_level = ENV.fetch('RAILS_LOG_LEVEL') { 'info' }.to_sym

  # Prepend all log lines with the following tags.
  config.log_tags = [:request_id]

  # The same image serves openipc.org and dev.openipc.org, so the permitted
  # host comes from the environment. The healthcheck is exempted because
  # container probes hit it by IP, which HostAuthorization would otherwise 403.
  config.hosts << ENV.fetch('APP_HOST') { 'openipc.org' }
  config.host_authorization = {
    exclude: ->(request) { request.path == '/up' }
  }

  # Use a different cache store in production.
  # config.cache_store = :mem_cache_store

  # Use a real queuing backend for Active Job (and separate queues per environment).
  # config.active_job.queue_adapter = :resque
  # config.active_job.queue_name_prefix = 'openipc_production'

  config.action_mailer.perform_caching = false

  # Ignore bad email addresses and do not raise email delivery errors.
  # Set this to true and configure the email server for immediate delivery to raise delivery errors.
  # config.action_mailer.raise_delivery_errors = false

  # Enable locale fallbacks for I18n (makes lookups for any locale fall back to
  # the I18n.default_locale when a translation cannot be found).
  config.i18n.fallbacks = true

  # Don't log any deprecations.
  config.active_support.report_deprecations = false

  # Use default logging formatter so that PID and timestamp are not suppressed.
  config.log_formatter = ::Logger::Formatter.new

  # Use a different logger for distributed setups.
  # require 'syslog/logger'
  # config.logger = ActiveSupport::TaggedLogging.new(Syslog::Logger.new 'app-name')

  # Default to STDOUT: in a container the log belongs to the runtime, not to a
  # file inside an ephemeral filesystem layer. Set RAILS_LOG_TO_STDOUT=0 to opt out.
  if ENV.fetch('RAILS_LOG_TO_STDOUT', '1') != '0'
    logger           = ActiveSupport::Logger.new(STDOUT)
    logger.formatter = config.log_formatter
    config.logger    = ActiveSupport::TaggedLogging.new(logger)
  end

  app_host = ENV.fetch('APP_HOST') { 'openipc.org' }
  config.default_url_options = { host: app_host }
  config.action_mailer.default_url_options = { host: app_host }
  config.action_mailer.delivery_method = :sendmail

  # Do not dump schema after migrations.
  config.active_record.dump_schema_after_migration = false
end
