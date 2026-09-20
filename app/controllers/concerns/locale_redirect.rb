# frozen_string_literal: true

# Sends ?locale=xx to the prefixed path, once and permanently (#154).
#
# Separate from Multilang, which decides which language to RENDER. This decides
# which URL a request should have been made to, which is a different question
# and the one with consequences outside the process: these are 301s, and a 301
# is a promise.
module LocaleRedirect
  extend ActiveSupport::Concern

  included do
    before_action :redirect_query_locale_to_path
  end

  private

  # ?locale=xx is retired: it answers 301 to the prefixed path (#154).
  #
  # This is the half of the change that cannot be taken back, because it moves
  # addresses other people have linked to. The other half -- the prefixed URLs,
  # the hreflang set and sitemap.xml -- shipped first and deliberately, so that
  # a ranking movement afterwards can be attributed to one or the other.
  #
  # Four conditions, each of which has a way of going wrong:
  #
  # GET only. The camera upload API is a POST to /snapshots and a redirect
  # would lose the body; firmware in the field cannot be upgraded, so that
  # request must never be touched.
  #
  # Not already prefixed, or /ru/donate?locale=ru redirects to itself forever.
  #
  # Only when the prefixed path actually ROUTES. The per-snapshot pages and the
  # 126-page catalogue are not localized yet -- their helpers take positional
  # arguments that :locale would swallow -- so redirecting /snapshots/123
  # ?locale=ru to /ru/snapshots/123 would send a Russian reader to the English
  # homepage via the catch-all. Those keep the query parameter and the session
  # until their routes are localized.
  #
  # The rest of the query string survives. Switching language on a filtered
  # list or a permanent camera link must not lose the filter or the
  # configuration, which is what the old switcher merge existed to protect.
  def redirect_query_locale_to_path
    return unless redirectable_query_locale?

    target = prefixed_path(params[:locale])
    return if target.nil?

    redirect_to with_remaining_query(target), status: :moved_permanently
  end

  def redirectable_query_locale?
    request.get? && request.path_parameters[:locale].blank? && available?(params[:locale])
  end

  def with_remaining_query(target)
    rest = request.query_parameters.except('locale')
    rest.any? ? "#{target}?#{rest.to_query}" : target
  end

  # The same page under the requested language, or nil when there is not one.
  #
  # English is the bare path, so ?locale=en asks for a URL that already exists
  # and the parameter can simply be dropped -- but ONLY on a page that has a
  # prefixed form at all. On one that does not, ?locale=en is the only thing
  # saying English, and dropping it hands the visitor back to the session and
  # the browser header: a Russian browser asking for /snapshots?locale=en got
  # Russian. The check has to be symmetrical with the one below it.
  def prefixed_path(wanted)
    return localized_page? ? request.path : nil if wanted.to_s == I18n.default_locale.to_s

    routable?(prefix(wanted)) ? prefix(wanted) : nil
  end

  def prefix(locale)
    request.path == '/' ? "/#{locale}" : "/#{locale}#{request.path}"
  end

  # Whether this page exists in any language other than the default. If it does
  # not, the locale cannot move into the path here and the old mechanism is
  # still the only one available.
  def localized_page?
    Multilang::LOCALES.keys.any? { |l| l.to_s != I18n.default_locale.to_s && routable?(prefix(l)) }
  end

  # recognize_path answers for everything, because this app has a catch-all --
  # an unrecognised path comes back as application#route_not_found rather than
  # raising. That is the case this has to detect.
  def routable?(path)
    Rails.application.routes.recognize_path(path, method: :get)[:action].to_s != 'route_not_found'
  rescue ActionController::RoutingError
    false
  end
end
