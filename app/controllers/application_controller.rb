# frozen_string_literal: true

class ApplicationController < ActionController::Base
  include RubyMineHacks if Rails.env.development?
  include Multilang
  # After Multilang: the redirect asks whether a prefixed route exists, and
  # Multilang owns the locale list it asks about (#154).
  include LocaleRedirect
  include RescueHandler

  protect_from_forgery unless: -> { request.format.json? }

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
