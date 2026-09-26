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

    # No prefix: the browser header decides, and nothing is remembered. The
    # session write that used to live here is gone (#155).
    #
    # It was the only cookie state on the public site, and it is why nothing
    # could be cached: a response carrying Set-Cookie is one most caches
    # decline to store, and one that varies by a cookie cannot be shared
    # between visitors at all. #154 removed the reason for it -- a reader who
    # picks a language now goes to /ru or /zh, and the address carries the
    # choice from page to page far better than a cookie did, because it
    # survives being shared, bookmarked and indexed.
    #
    # ?locale= is still honoured for a route that has no prefixed form, but
    # only for that request. Sticking it in the session
    # would put the cookie back for every page after it.
    # The explicit default matters. browser_locale returns nil when the header
    # names nothing this site serves, and I18n.with_locale(nil) does not set a
    # locale -- it leaves whatever the thread was last used for, which is the
    # leak the comment above this method is about.
    requested = params[:locale] if available?(params[:locale])
    chosen = requested || browser_locale || I18n.default_locale

    negotiated = I18n.with_locale(chosen, &)
    vary_by_accept_language
    negotiated
  end

  # An unprefixed path chooses its language from Accept-Language (and, until
  # #155, from the session), so one URL answers in three languages depending on
  # who asks. Rails already sends `Vary: Accept` from format negotiation, and
  # Accept-Language is not covered by it: a shared cache that stored this
  # response would be entitled to hand the first visitor's language to everyone
  # behind it.
  #
  # Nothing stores it today -- Cache-Control is `max-age=0, private,
  # must-revalidate` -- so this changes no behaviour now. It is a precondition
  # for #155, which adds real cache headers, and for the mirrors gaining
  # proxy_cache in Phase 7; by then the declaration has to already be true, and
  # a wrong Vary is invisible until a Russian visitor is served a Chinese page.
  #
  # /ru and /zh deliberately do not get this. Their language is in the address,
  # which is exactly the property that makes them cacheable, and claiming they
  # vary by a header they ignore would throw that away.
  def vary_by_accept_language
    values = response.headers['Vary'].to_s.split(',').map(&:strip).reject(&:empty?)
    return if values.include?('*') || values.any? { |value| value.casecmp?('Accept-Language') }

    response.headers['Vary'] = (values + ['Accept-Language']).join(', ')
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
