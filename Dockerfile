# syntax=docker/dockerfile:1
#
# openipc.org production image.
#
# Debian, deliberately not Alpine: Gemfile.lock resolves only x86_64-linux and
# pins nokogiri 1.15.4-x86_64-linux, which is the precompiled *glibc* build and
# will not load against musl.

ARG RUBY_VERSION=3.3.12
ARG NODE_MAJOR=20

# --------------------------------------------------------------------------
# Build stage
# --------------------------------------------------------------------------
FROM ruby:${RUBY_VERSION}-slim-bookworm AS build
ARG NODE_MAJOR

ENV RAILS_ENV=production \
    BUNDLE_DEPLOYMENT=1 \
    BUNDLE_PATH=/usr/local/bundle \
    BUNDLE_WITHOUT=development:test

# g++ is not optional: sassc compiles libsass from source.
RUN apt-get update -qq && apt-get install --no-install-recommends -y \
      build-essential \
      ca-certificates \
      curl \
      default-libmysqlclient-dev \
      git \
      gnupg \
      libffi-dev \
      libssl-dev \
      libyaml-dev \
      pkg-config \
      zlib1g-dev \
  && curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash - \
  && apt-get install --no-install-recommends -y nodejs \
  && rm -rf /var/lib/apt/lists/*

# The project is on Yarn 4 (Berry) -- yarn.lock carries the __metadata header,
# and a v1 yarn cannot read it. corepack resolves the exact version from the
# "packageManager" field in package.json.
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable

WORKDIR /rails

# Gems first, so a source-only change does not re-resolve the bundle.
COPY Gemfile Gemfile.lock ./
RUN bundle install \
  && rm -rf "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git

# Then JS deps, same reasoning. --immutable is Berry's --frozen-lockfile.
COPY package.json yarn.lock .yarnrc.yml ./
RUN yarn install --immutable

COPY . .

# esbuild + sass produce app/assets/builds/, which Sprockets then digests.
#
# Two throwaway values are needed for this step and only this step:
#   SECRET_KEY_BASE_DUMMY  relaxes require_master_key (see production.rb)
#   SECRET_KEY_BASE        satisfies the production environment itself --
#                          Rails 7.0 has no dummy-secret mechanism, that
#                          arrived in 7.1
# Neither is baked into the image or used at runtime; the real key arrives
# as RAILS_MASTER_KEY from the host env file.
RUN yarn build \
  && yarn build:css \
  && SECRET_KEY_BASE_DUMMY=1 \
     SECRET_KEY_BASE=precompile_placeholder_not_used_at_runtime \
     bundle exec rails assets:precompile \
  && rm -rf node_modules tmp/cache

# --------------------------------------------------------------------------
# Runtime stage
# --------------------------------------------------------------------------
FROM ruby:${RUBY_VERSION}-slim-bookworm AS runtime

ENV RAILS_ENV=production \
    BUNDLE_DEPLOYMENT=1 \
    BUNDLE_PATH=/usr/local/bundle \
    BUNDLE_WITHOUT=development:test \
    RAILS_SERVE_STATIC_FILES=1 \
    RAILS_LOG_TO_STDOUT=1 \
    PORT=3000

# libvips42 must be built with libheif -- Snapshot documents that HEIF decoding
# depends on it, and without it ProcessImagesJob fails silently on HEIF uploads.
# tzdata is required because tzinfo-data is bundled only for windows/jruby.
# msmtp provides the sendmail binary ActionMailer shells out to.
RUN apt-get update -qq && apt-get install --no-install-recommends -y \
      ca-certificates \
      curl \
      libffi8 \
      libheif1 \
      libjemalloc2 \
      libmariadb3 \
      libvips42 \
      libyaml-0-2 \
      msmtp-mta \
      tzdata \
  && rm -rf /var/lib/apt/lists/*

# Measured on this host before changing anything (#148, hourly series in
# /var/log/openipc-rss.log): anon memory is 0.27 GiB on a fresh boot, 1.56 GiB
# an hour later, 2.94 GiB at nine hours and 3.19 GiB at 4.9 days. That shape is
# not a leak -- nine tenths of the growth happens on the first day and then it
# asymptotes -- it is glibc handing each of the 32 Puma threads its own arena
# and never giving the pages back.
#
# Asserted at build time, because the failure mode is silent: a missing library
# makes LD_PRELOAD a no-op, the process keeps running on glibc, and the only
# symptom is memory drifting back to where it was. Better a red build when a
# base image moves the library than an image that quietly stops doing the one
# thing it was changed to do.
RUN LD_PRELOAD=libjemalloc.so.2 ruby -e \
      'abort "jemalloc did not preload" unless File.read("/proc/self/maps").include?("jemalloc")'

# After the install, never before it: as an ENV this applies to every later
# RUN as well, and a preload naming a library that is not there yet makes the
# loader complain on every command in the build.
#
# Bare soname rather than a path -- the library lives under the multiarch
# directory and the loader finds it by name, so this does not have to know
# whether the image was built for amd64 or arm64.
#
# MALLOC_CONF is not optional and is the whole reason this is safe. Measured in
# this image, 32 threads churning 5MB strings, RSS after the work and again
# twenty seconds later:
#
#   glibc                          18.7 MB -> 18.5 MB
#   jemalloc, default config      360   MB -> 345   MB     never returned
#   jemalloc, the settings below  316   MB -> 21    MB
#
# Left at its defaults, jemalloc purges a dirty page only when something else
# allocates in the same arena, so an idle worker holds everything it ever
# peaked at. background_thread gives the decay a clock of its own instead.
# Shipping jemalloc without this would have been a memory regression, not a
# saving, which is the opposite of what #148 set out to do.
#
# MALLOC_ARENA_MAX is deliberately NOT set. It is glibc's, so it is inert
# whenever the preload works; and on the one workload here that fragments --
# many small allocations across 32 threads -- capping glibc at two arenas
# retained 123MB against 72MB for the default. A fallback that is measurably
# worse than doing nothing is not a fallback.
ENV LD_PRELOAD=libjemalloc.so.2 \
    MALLOC_CONF=background_thread:true,dirty_decay_ms:1000,muzzy_decay_ms:1000

WORKDIR /rails

COPY --from=build "${BUNDLE_PATH}" "${BUNDLE_PATH}"
COPY --from=build /rails /rails

# Run unprivileged. The bare-metal service ran Puma as root against a
# paul-owned tree for no reason; do not carry that forward.
RUN groupadd --system --gid 1000 rails \
  && useradd --system --uid 1000 --gid 1000 --create-home rails \
  && mkdir -p log tmp/pids tmp/cache storage public/files \
  && chown -R rails:rails log tmp storage public
USER rails:rails

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
  CMD curl -fsS http://localhost:3000/up || exit 1

CMD ["bundle", "exec", "puma", "-C", "config/puma.rb"]
