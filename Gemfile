source 'https://rubygems.org'

ruby '3.3.12'

# Bundle edge Rails instead: gem 'rails', github: 'rails/rails', branch: 'main'
gem 'rails', '~> 8.1.0'

# The original asset pipeline for Rails [https://github.com/rails/sprockets-rails]
gem 'sprockets-rails'

# Use mysql as the database for Active Record
gem 'mysql2', '~> 0.5'

# Use the Puma web server [https://github.com/puma/puma]
gem 'puma'# , '>= 5.0'

# Rack 2, deliberately. Rails 7.1 widened its constraint to `rack >= 2.2.4`, so
# an unpinned bundle resolves Rack 3 -- and Puma 5 refuses to boot against it
# ("Puma 5 is not compatible with Rack 3"). Taking Rack 3 means taking Puma 6
# and the header-casing change with it, and this app sets X-Sendfile-Type and
# X-Accel-Mapping by hand for firmware downloads; getting that location wrong
# took both sites down to unstyled text on 2026-08-24.
#
# This pin is what makes Rack 3 separable. Rails 8.1 runs against Rack 2.2 and
# Puma 5 quite happily, so the framework upgrade and the Rack migration do not
# have to be the same change -- which is the opposite of what #153 assumed when
# it put Rails 8 outside the epic.
gem 'rack', '~> 2.2'

# json 2, for the same reason. Ruby 3.3 ships json 2.7 as a default gem, but a
# transitive dependency resolves 3.x, and json 3.0 removed the `quirks_mode`
# keyword that ActiveSupport's JSON encoder still passes -- retested with the
# pin lifted on 8.1.3.1, where it fails 27 tests. The failure is not obviously
# about json: `bin/rails`
# aborts with "unknown keyword: quirks_mode" while parsing config/database.yml,
# because the production password goes through to_json there.
gem 'json', '~> 2.7'

# Bundle and transpile JavaScript [https://github.com/rails/jsbundling-rails]
gem 'jsbundling-rails'

# Hotwire's SPA-like page accelerator [https://turbo.hotwired.dev]
gem 'turbo-rails'

# Hotwire's modest JavaScript framework [https://stimulus.hotwired.dev]
gem 'stimulus-rails'

# Bundle and process CSS [https://github.com/rails/cssbundling-rails]
gem 'cssbundling-rails'

# Build JSON APIs with ease [https://github.com/rails/jbuilder]
gem 'jbuilder'

# Use Redis adapter to run Action Cable in production
# gem 'redis', '>= 4.0.1'

# Use Kredis to get higher-level data types in Redis [https://github.com/rails/kredis]
# gem 'kredis'

# Use Active Model has_secure_password [https://guides.rubyonrails.org/active_model_basics.html#securepassword]
# gem 'bcrypt', '~> 3.1.7'

# Windows does not include zoneinfo files, so bundle the tzinfo-data gem
gem 'tzinfo-data', platforms: %i[windows jruby]

# Reduces boot times through caching; required in config/boot.rb
gem 'bootsnap', require: false

# Use Active Storage variants [https://guides.rubyonrails.org/active_storage_overview.html#transforming-images]
gem 'image_processing', '~> 1.2'

group :development, :test do
  # See https://guides.rubyonrails.org/debugging_rails_applications.html#debugging-with-the-debug-gem
  gem 'debug', platforms: %i[mri windows]
end

group :development do
  # Use console on exceptions pages [https://github.com/rails/web-console]
  gem 'web-console'

  # Add speed badges [https://github.com/MiniProfiler/rack-mini-profiler]
  # gem 'rack-mini-profiler'

  # Speed up commands on slow machines / big apps [https://github.com/rails/spring]
  # gem 'spring'

  # error_highlight is deliberately absent. It is a default gem from Ruby 3.2
  # onward, and declaring it means Bundler insisting on the locked version after
  # Ruby has already activated its own -- which fails in whichever direction the
  # two disagree. On 3.1 that was "already activated 0.3.0, Gemfile requires
  # 0.5.1", worked around with RUBYOPT=--disable-error_highlight in
  # docker/Dockerfile.dev; on 3.3 it was the same error with 0.6.0 and 0.5.1
  # swapped. Ruby ships a good version; let it.

  gem 'activerecord-reset-pk-sequence'
  gem 'easy_translate', '~> 0.5.1'
  gem 'i18n-tasks'
  gem 'rubocop'
  # .rubocop.yml has `require: rubocop-performance`, so rubocop cannot start
  # without this -- it was required by the config but never listed here.
  gem 'rubocop-performance'
  # gem 'rubocop-rails'
  # gem 'ruby-debug-ide'
end

group :test do
  # Use system testing [https://guides.rubyonrails.org/testing.html#system-testing]
  gem 'capybara'

  # Minitest 5, pinned, as minitest's own post-install message asks for. Rails
  # only asks for >= 5.1, so an unpinned bundle takes minitest 6, which moved
  # minitest/mock out into a gem of its own -- and camera_test.rb and
  # release_cache_test.rb both require it, for `stub`. Minitest 6 also drops
  # Minitest::Unit and `assert_equal nil`, so it is its own migration and not a
  # side effect of a Rails upgrade.
  gem 'minitest', '~> 5.0'

  gem 'selenium-webdriver'
end

gem 'activestorage-validator', '~> 0.2.2'
gem 'bootstrap_form', '~> 5.4'
gem 'bootstrap5-kaminari-views', '~> 0.0.1'
# 4.9.4, not 4.8: earlier Devise reads Rails.application.secrets, which Rails 7.1
# deprecates and 7.2 removes. Nothing in this app calls it -- the warning comes
# from inside the gem.
gem 'devise', '~> 4.9.4'
gem 'kaminari', '~> 1.2'
gem 'sassc-rails'
# libvips comes from the OS package (libvips42 + libheif1), not from a gem.
# The 'vips' gem ships prebuilt libvips binaries and shadowed the system copy;
# it also compiles from source at install time, which needs wget in the image.
