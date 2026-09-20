# frozen_string_literal: true

class ApplicationController < ActionController::Base
  include RubyMineHacks if Rails.env.development?
  include Multilang
  # After Multilang: the redirect asks whether a prefixed route exists, and
  # Multilang owns the locale list it asks about (#154).
  include LocaleRedirect
  include RescueHandler

  protect_from_forgery unless: -> { request.format.json? }

  # Rails writes session[:_csrf_token] the first time a token is asked for, and
  # `csrf_meta_tags` in the layout asked for one on every page -- so after the
  # locale write went (#155), this was still putting Set-Cookie on responses
  # that have no form on them at all, and a response carrying Set-Cookie is one
  # most caches decline to store.
  #
  # The token is emitted where something will actually post: the wizard, which
  # has the only public form, and the Devise pages, which share this layout
  # because there is no layouts/devise. Everything else -- the marketing pages,
  # the catalogue, the Open Wall -- is read-only and needs none.
  #
  # Narrow rather than clever on purpose. A page that grows a form and forgets
  # this fails loudly on the first submission with an InvalidAuthenticityToken,
  # which is a better failure than a silently uncacheable site.
  helper_method :csrf_needed?

  def csrf_needed?
    respond_to?(:devise_controller?, true) && devise_controller?
  end

  add_flash_types :alert, :notice, :danger, :info, :success, :warning

  # Tell the cache in front of us that this response was rendered for a
  # signed-in admin and must not be stored.
  #
  # nginx cannot work this out for itself. Devise keeps the admin identity
  # inside the same encrypted `_openipc_session` cookie that every visitor
  # gets, so no variable available to it distinguishes an admin's request from
  # anyone else's -- and refusing to cache every response carrying a session
  # cookie would mean refusing to cache anything at all, which is what the
  # microcache exists to avoid.
  #
  # Without this, `snapshots/show` -- which renders the uploading camera's IP
  # and MAC address for admins, and is cached for 300s on the path alone --
  # stores whatever an admin was shown and serves it to the public until the
  # entry expires. The same template is reached at /open-wall/camera/<id>,
  # cached for 60s, so both paths need it.
  #
  # It is set for every action rather than that one view, because the rule is
  # about admin-conditional content in general and the next such block will not
  # come with a reminder. `proxy_no_cache` in
  # deploy/nginx/sites-available/org.openipc is the other half.
  after_action :refuse_shared_caching_for_admins

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
  # Only GET, only 200, and never for an admin -- a signed-in admin sees
  # uploader IPs and MAC addresses on the same URLs, which is what
  # X-Admin-View above is for. Anything that sets a flash is skipped too: a
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

    f = freshness
    response.set_header('Cache-Control',
                        "public, max-age=#{f[:max_age]}, stale-while-revalidate=#{f[:swr]}")
  end

  # Only GET, only 200, and never for a signed-in admin -- they see uploader
  # IPs and MAC addresses on URLs an anonymous visitor also reaches. A response
  # carrying a flash is skipped too: a one-shot message must not be stored and
  # handed to the next reader.
  def publicly_cacheable?
    return false unless request.get? && response.status == 200
    return false if admin_signed_in? || flash.any?

    !(respond_to?(:devise_controller?, true) && devise_controller?)
  end

  def freshness
    FRESHNESS["#{controller_path}##{action_name}"] ||
      FRESHNESS[controller_path] ||
      DEFAULT_FRESHNESS
  end

  def refuse_shared_caching_for_admins
    response.set_header('X-Admin-View', '1') if admin_signed_in?
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
