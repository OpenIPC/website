# syntax=docker/dockerfile:1
#
# openipc.org production image.
#
# Debian, deliberately not Alpine: Gemfile.lock resolves only x86_64-linux and
# pins nokogiri 1.15.4-x86_64-linux, which is the precompiled *glibc* build and
# will not load against musl.

ARG RUBY_VERSION=3.1.2
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
  && npm install -g yarn@1.22.22 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /rails

# Gems first, so a source-only change does not re-resolve the bundle.
COPY Gemfile Gemfile.lock ./
RUN bundle install \
  && rm -rf "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git

# Then JS deps, same reasoning.
COPY package.json yarn.lock ./
RUN yarn install --frozen-lockfile

COPY . .

# esbuild + sass produce app/assets/builds/, which Sprockets then digests.
# SECRET_KEY_BASE_DUMMY relaxes require_master_key for this step only (see
# config/environments/production.rb) so the real key is never needed to build.
RUN yarn build \
  && yarn build:css \
  && SECRET_KEY_BASE_DUMMY=1 bundle exec rails assets:precompile \
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
      libmariadb3 \
      libvips42 \
      libyaml-0-2 \
      msmtp-mta \
      tzdata \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /rails

COPY --from=build "${BUNDLE_PATH}" "${BUNDLE_PATH}"
COPY --from=build /rails /rails

# Run unprivileged. The bare-metal service ran Puma as root against a
# paul-owned tree for no reason; do not carry that forward.
RUN groupadd --system --gid 1000 rails \
  && useradd --system --uid 1000 --gid 1000 --create-home rails \
  && mkdir -p log tmp storage public/files \
  && chown -R rails:rails log tmp storage public/files
USER rails:rails

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
  CMD curl -fsS http://localhost:3000/up || exit 1

CMD ["bundle", "exec", "puma", "-C", "config/puma.rb"]
