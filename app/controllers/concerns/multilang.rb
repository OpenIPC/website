# frozen_string_literal: true

module Multilang
  extend ActiveSupport::Concern

  # Kept in step with I18n.available_locales by the test below it. This used to
  # list ten languages plus Japanese, which had no locale file and was never in
  # available_locales at all -- the switcher would have offered a language the
  # app could not render.
  LOCALES = {
    en: 'English',
    ru: 'Русский',
    zh: '中文'
  }.freeze

  # The locales that appear as a path prefix, for the route constraint in
  # config/routes.rb. English is absent on purpose: it is served at the bare
  # path, so /donate stays /donate forever and no indexed URL moves (#154).
  # Here rather than in routes.rb so there is one list, and so defining it does
  # not happen inside the routes.draw block.
  IN_PATH = Regexp.union(LOCALES.keys.reject { |l| l == :en }.map(&:to_s)).freeze

  included do
    before_action :redirect_query_locale_to_path
    around_action :set_locale

    helper_method :browser_locale
    helper_method :locales_for_select
    helper_method :locale_path
    helper_method :locale_alternates
  end

  # The visitor's most-preferred language that this site can actually render.
  #
  # This used to be `scan(/[a-z]{2}(?=;)/)`, which only sees a tag followed by a
  # `;` -- that is, a tag carrying a q-value. The first entry in an
  # Accept-Language header does not carry one, so the visitor's *top* preference
  # was the one entry the scan could never match: `ru,en;q=0.9` answered `en`.
  # Nothing noticed, because set_locale was switched off.
  def browser_locale
    accepted_languages.find { |tag| I18n.available_locales.include?(tag.to_sym) }
  end

  # The header's language tags, most-preferred first, minus the ones the client
  # has ruled out. `q=0` means "not acceptable" (RFC 9110 12.4.2), so
  # `ru;q=0,de;q=0.9` must not answer `ru` merely because `de` is not served.
  def accepted_languages
    entries = request.env['HTTP_ACCEPT_LANGUAGE'].to_s.split(',')
    ranked = entries.map.with_index { |part, index| rank(part, index) }
    ranked.reject { |_, quality, _| quality.zero? }
          .sort_by { |entry| entry.drop(1) }
          .map(&:first)
  end

  # One header entry, as [tag, -quality, index].
  #
  # The tag is cut to the two letters I18n keys on, so `de-DE` ranks as `de`.
  # An entry with no q-value is the strongest the header carries, so it defaults
  # to 1 rather than being skipped -- skipping it is what the old scan did.
  # index breaks ties, because sort_by is not stable and equal q-values have to
  # keep the order the browser sent them in.
  def rank(part, index)
    tag, *parameters = part.split(';')
    [tag.to_s.strip.downcase[0, 2].to_s, -quality_of(parameters), index]
  end

  # Splitting on the literal ';q=' missed every header that spells it another
  # way, and the grammar allows several: whitespace around the separator, and
  # `Q` as readily as `q`. `en; q=0.1,ru;q=0.9` ranked English at 1 and picked
  # it -- the opposite of what the visitor asked for.
  def quality_of(parameters)
    found = parameters.map(&:strip).find { |parameter| parameter.downcase.start_with?('q=') }
    return 1.0 if found.nil?

    found[2..].to_f
  end

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
  def prefixed_path(wanted)
    return request.path if wanted.to_s == I18n.default_locale.to_s

    candidate = request.path == '/' ? "/#{wanted}" : "/#{wanted}#{request.path}"
    routable?(candidate) ? candidate : nil
  end

  # recognize_path answers for everything, because this app has a catch-all --
  # an unrecognised path comes back as application#route_not_found rather than
  # raising. That is the case this has to detect.
  def routable?(path)
    Rails.application.routes.recognize_path(path, method: :get)[:action].to_s != 'route_not_found'
  rescue ActionController::RoutingError
    false
  end

  # Where the locale comes from, in the order that wins.
  #
  # The path is authoritative: /ru/donate is Russian whatever the session says,
  # because the URL is now the address of the page rather than a hint about it.
  # A shared cache can only key on the URL, so anything the URL does not carry
  # cannot be allowed to change the response.
  # request.path_parameters, not params: `?locale=zh` also lands in params, and
  # the two must not be confused. The path is the new authority; the query
  # parameter is the old mechanism, still honoured until #155 retires it, and
  # it still writes the session so a ?locale= link in the wild keeps working
  # exactly as it did. Removing that before the prefixes are linked everywhere
  # would give those visitors one page in their language and then English.
  def locale_from_path
    from_route = request.path_parameters[:locale]
    available?(from_route) ? from_route : nil
  end

  # Every route helper gains the prefix without being told, for as long as the
  # current locale is not the default one.
  #
  # This used to be `def self.default_url_options` inside a module that is
  # `include`d, which defines a singleton method on the module object. Rails
  # calls the INSTANCE method, so it was never called at all -- which is why
  # the keep_query lambda in config/routes.rb had to exist, and why the session
  # cookie was the only thing carrying a language between pages.
  def default_url_options
    return {} if I18n.locale.to_s == I18n.default_locale.to_s

    { locale: I18n.locale }
  end

  # For the links that are written as literal strings rather than built from a
  # route helper -- about forty-five of them across the templates. English is
  # unprefixed, so this returns the path untouched and no English URL moves.
  # Paths that are not pages and must never gain a prefix. /ru/fonts/... and
  # /ru/assets/... are 404s, and the first draft of this produced both by
  # rewriting the font preloads in the layout.
  NOT_A_PAGE = %r{\A/(assets|fonts|files|dl|wall|images|rails|admin|up)(/|\z)}
  ALREADY_PREFIXED = %r{\A/(#{LOCALES.keys.join('|')})(/|\z)}

  def locale_path(path)
    return path if I18n.locale.to_s == I18n.default_locale.to_s

    prefixable?(path.to_s) ? "/#{I18n.locale}#{path}" : path
  end

  def prefixable?(path)
    return false unless path.start_with?('/')
    return false if path.match?(NOT_A_PAGE) || path.match?(ALREADY_PREFIXED)

    # A file rather than a page: /images/logo_openipc.png, /ru/installation.md.
    File.extname(path.split('?').first.to_s).blank?
  end

  # around_action, not before_action, and I18n.with_locale rather than
  # `I18n.locale =`. I18n.locale is per-thread and nothing resets it at the end
  # of a request, so an assignment leaks into whatever that thread serves next.
  # Every request does pass through here, so in practice it would be overwritten
  # -- but "in practice" is doing the work in that sentence, and with_locale
  # costs nothing.
  def set_locale(&)
    # The path wins outright when it carries a locale, and deliberately does not
    # touch the session: /ru/donate is Russian for anyone who opens it, and it
    # must render the same for everyone or a shared cache cannot store it.
    from_path = locale_from_path
    return I18n.with_locale(from_path, &) if from_path

    # No prefix: the behaviour that was here before, unchanged. A first visit
    # starts from what the browser asks for; later visits keep whatever the
    # switcher last set; ?locale=xx still overrides and still sticks. #155
    # removes this half, once the prefixed URLs are the ones being linked.
    session[:locale] ||= browser_locale
    session[:locale] = I18n.default_locale unless available?(session[:locale])
    session[:locale] = params[:locale] if available?(params[:locale])

    I18n.with_locale(session[:locale], &)
  end

  def available?(locale)
    locale.present? && I18n.available_locales.include?(locale.to_s.to_sym)
  end

  # The same page in every language this site serves, for the hreflang block in
  # the layout and for sitemap.xml. Search engines need each alternate to point
  # at the others, including back at itself, or they treat them as unrelated
  # pages that happen to look similar.
  def locale_alternates(path = nil)
    here = path || request.path.sub(%r{\A/(#{LOCALES.keys.join('|')})(?=/|\z)}, '')
    here = '/' if here.empty?
    I18n.available_locales.to_h do |l|
      prefix = l.to_s == I18n.default_locale.to_s ? '' : "/#{l}"
      [l, "#{prefix}#{here == '/' ? '' : here}".presence || '/']
    end
  end

  # LOCALES, not `t("locales.#{l}")`: there are no `locales.*` keys in any file,
  # so every entry came back as a translation-missing span. A language name is
  # written the same in every language anyway, which is why the switcher
  # partial uses LOCALES too.
  def locales_for_select
    I18n.available_locales.map { |l| [LOCALES[l], l] }
  end
end
