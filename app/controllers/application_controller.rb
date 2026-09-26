# frozen_string_literal: true

class ApplicationController < ActionController::Base
  include Multilang
  # After Multilang: the redirect asks whether a prefixed route exists, and
  # Multilang owns the locale list it asks about (#154).
  include LocaleRedirect
  include RescueHandler

  protect_from_forgery unless: -> { request.format.json? }

  add_flash_types :alert, :notice, :danger, :info, :success, :warning

  # Say how long a response is good for (#155). Until now every public page
  # answered `max-age=0, private, must-revalidate` -- Rails' default for a
  # response it knows nothing about -- so the only caching on this site was
  # nginx overriding that with proxy_ignore_headers, which is a cache guessing
  # rather than being told.
  #
  # The numbers come from how often each thing actually changes. The Open Wall
  # gains a snapshot every few minutes; the catalogue has not changed in eight
  # months; the marketing pages change when someone edits them. The long
  # stale-while-revalidate is the part that matters under load: it lets an edge
  # keep answering from a slightly old copy while it fetches a new one, which
  # is exactly the behaviour the 2026-08-30 flood needed and did not have.
  #
  # Only GET and only 200. Anything that sets a flash is skipped too: a
  # one-shot message must not be stored and handed to the next reader.
  # Keyed on controller#action, not controller: /open-wall and
  # /snapshots/<id> are both SnapshotsController and want different answers.
  # The gallery gains a snapshot every few minutes; an individual snapshot
  # never changes once uploaded.
  #
  # These numbers deliberately match the proxy_cache_valid already in
  # deploy/nginx/sites-available/org.openipc, because nginx stops overriding
  # them in this same change: with proxy_ignore_headers gone, whatever Rails
  # says here IS the cache lifetime. Declaring 60s for a page the vhost had
  # been holding 300s would have quietly multiplied that flood's cost by five.
  FRESHNESS = {
    'snapshots#index' => { max_age: 60, swr: 600 },
    'snapshots#camera' => { max_age: 60, swr: 600 },
    'snapshots#show' => { max_age: 300, swr: 3600 },
    'snapshots#oneday' => { max_age: 300, swr: 3600 },
    'cameras/socs' => { max_age: 3600, swr: 86_400 },
    'cameras/vendors' => { max_age: 3600, swr: 86_400 },
    'sitemaps' => { max_age: 3600, swr: 86_400 }
  }.freeze
  DEFAULT_FRESHNESS = { max_age: 300, swr: 3600 }.freeze

  after_action :declare_freshness

  def declare_freshness
    return unless publicly_cacheable?

    f = capped_freshness(freshness)
    response.set_header('Cache-Control',
                        "public, max-age=#{f[:max_age]}, stale-while-revalidate=#{f[:swr]}")
  end

  # A page must not be served after a number printed on it has expired (#198).
  #
  # The backer count is good for 48 hours and the catalogue's policy is an hour
  # fresh plus a day stale, so a response built shortly before the data expired
  # could be handed out for another twenty-five -- which would make the 48-hour
  # rule one about rendering rather than about what a reader sees.
  #
  # The window is recorded by shared/_support_count while it renders, because
  # the view is the only place that knows the partial ran at all, and it is nil
  # in the ordinary case: the cron is hourly, so there are normally 47 hours of
  # headroom against 25 of exposure and nothing to cap.
  SUPPORT_WINDOW_KEY = 'openipc.support_count_window'

  # Callable from a view, which is where it is known.
  def note_support_count_window(seconds)
    request.env[SUPPORT_WINDOW_KEY] = seconds
  end
  helper_method :note_support_count_window

  def capped_freshness(fresh)
    window = request.env[SUPPORT_WINDOW_KEY]
    return fresh if window.nil?
    return { max_age: 0, swr: 0 } if window <= 0

    { max_age: [fresh[:max_age], window].min,
      swr: [fresh[:swr], window - [fresh[:max_age], window].min].min }
  end

  # Only GET, only 200, and nothing carrying a flash: a one-shot message must
  # not be stored and handed to the next reader.
  #
  # There used to be two more exclusions -- a signed-in admin, who saw uploader
  # IPs and MAC addresses on public URLs, and any page that minted a CSRF token
  # and so wrote the session. Both went with the admin (#288): no page left
  # has a form that posts, and nobody signs in.
  def publicly_cacheable?
    return false unless request.get? && response.status == 200
    return false if flash.any?

    # An action that declared its own freshness has said something more
    # specific than this rule can: /cameras/socs.json asks for five minutes
    # and an ETag, where the catalogue pages around it want an hour. Rails
    # leaves cache_control empty unless expires_in or fresh_when set it, so
    # this distinguishes "declared" from "defaulted" without a flag.
    response.cache_control.blank?
  end

  def freshness
    FRESHNESS["#{controller_path}##{action_name}"] ||
      FRESHNESS[controller_path] ||
      DEFAULT_FRESHNESS
  end

  # This used to append every unmatched URL, and the referer that produced it,
  # to public/notfound.txt -- a file in the directory the app serves, so
  # https://openipc.org/notfound.txt answered 200 to anyone who asked. A
  # referer says where a visitor came from, which is not ours to publish, and
  # the accumulated list is a map of what people probe. It reached 966 lines
  # inside three hours of one deploy, and nothing ever read it.
  #
  # Nothing replaces it here, because nothing needs to: nginx's combined log
  # already records the path, the status and the referer for every request, in
  # a rotated file on the host rather than a world-readable one in public/.
  # Writing it again would duplicate that at info level for every miss.
  # The catch-all every mistyped and every retired URL lands on. Sending the
  # reader of /ru/<typo> to the English homepage is the complaint #154 exists
  # to fix, in its most visible form.
  #
  # The prefix is read off the path rather than taken from I18n.locale, and the
  # difference matters. This route is a glob and cannot sit inside
  # `scope "(:locale)"`, so path_parameters carries no :locale here and
  # I18n.locale has fallen back to the session or the browser header -- which
  # would send someone who mistyped an English URL to /ru because of a header
  # they never chose. #154's rule is that the address decides the language, so
  # a prefixed path keeps its prefix and a bare one stays bare.
  def route_not_found
    prefix = request.path[%r{\A/(#{Multilang::IN_PATH})(?=/|\z)}, 1]

    redirect_to prefix ? "/#{prefix}" : '/'
  end
end
