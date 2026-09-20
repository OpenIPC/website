# frozen_string_literal: true

# Tell browsers they may keep the fingerprinted assets.
#
# Before this, a digested asset answered with `last-modified` and nothing else
# -- no Cache-Control, no ETag -- so every repeat visitor revalidated every
# stylesheet, script and font on every page. 27,676 asset requests a day
# against roughly 1,600 humans (#149).
#
# A middleware rather than `config.public_file_server.headers`, which is the
# obvious one-liner and is wrong here: it applies to everything under public/,
# and that includes robots.txt, the static 404/422/500 pages, favicon.ico and
# og-default.png. Telling the world to cache robots.txt for a year, immutably,
# is not a change anyone can take back inside a year.
#
# The two paths get different answers because only one of them is fingerprinted:
#
# * /assets/ is digested by Sprockets and `config.assets.compile = false`, so
#   a changed file is a changed URL and `immutable` is exactly true -- the
#   browser may skip revalidation entirely, even on a forced reload.
#
# * /fonts/ is not. tools/copy-fonts.mjs copies them out of node_modules under
#   their own names because public/ does not go through Sprockets, so the URL
#   survives a version bump. A month is long enough to matter and short enough
#   that replacing a font is not a year-long commitment.
class AssetCacheControl
  IMMUTABLE = 'public, max-age=31536000, immutable'
  REVALIDATED_MONTHLY = 'public, max-age=2592000'

  CACHEABLE = {
    '/assets/' => IMMUTABLE,
    '/fonts/' => REVALIDATED_MONTHLY
  }.freeze

  # Only what was actually served. A 404 for a missing asset must stay
  # uncacheable, or a deploy that briefly races the digest manifest pins the
  # miss into every cache between here and the visitor.
  SERVED = [200, 304].freeze

  def initialize(app)
    @app = app
  end

  def call(env)
    status, headers, body = @app.call(env)
    value = CACHEABLE.find { |prefix, _| env['PATH_INFO'].to_s.start_with?(prefix) }&.last
    headers['Cache-Control'] = value if value && SERVED.include?(status)
    [status, headers, body]
  end
end
