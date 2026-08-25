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

  included do
    around_action :set_locale

    helper_method :browser_locale
    helper_method :locales_for_select
    helper_method :locale_switcher
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

  # The header's language tags, most-preferred first.
  def accepted_languages
    entries = request.env['HTTP_ACCEPT_LANGUAGE'].to_s.split(',')
    ranked = entries.map.with_index { |part, index| rank(part, index) }
    ranked.sort_by { |entry| entry.drop(1) }.map(&:first)
  end

  # One header entry, as [tag, -quality, index].
  #
  # The tag is cut to the two letters I18n keys on, so `de-DE` ranks as `de`.
  # An entry with no `;q=` is the strongest the header carries, so it defaults
  # to 1 rather than being skipped -- skipping it is what the old scan did.
  # index breaks ties, because sort_by is not stable and equal q-values have to
  # keep the order the browser sent them in.
  def rank(part, index)
    tag, quality = part.split(';q=')
    [tag.to_s.strip.downcase[0, 2].to_s, -(quality || '1').to_f, index]
  end

  def self.default_url_options
    { locale: I18n.locale }
  end

  # around_action, not before_action, and I18n.with_locale rather than
  # `I18n.locale =`. I18n.locale is per-thread and nothing resets it at the end
  # of a request, so an assignment leaks into whatever that thread serves next.
  # Every request does pass through here, so in practice it would be overwritten
  # -- but "in practice" is doing the work in that sentence, and with_locale
  # costs nothing.
  def set_locale(&)
    # A first visit has no choice recorded, so start from what the browser asks
    # for. Later visits keep whatever the switcher last set.
    session[:locale] ||= browser_locale
    session[:locale] = I18n.default_locale unless available?(session[:locale])

    # The switcher links to ?locale=xx. A locale this site does not serve is
    # ignored rather than honoured: it used to be able to leave I18n.locale set
    # to something with no translation file behind it.
    session[:locale] = params[:locale] if available?(params[:locale])

    I18n.with_locale(session[:locale], &)
  end

  def available?(locale)
    locale.present? && I18n.available_locales.include?(locale.to_s.to_sym)
  end

  # LOCALES, not `t("locales.#{l}")`: there are no `locales.*` keys in any file,
  # so every entry came back as a translation-missing span. A language name is
  # written the same in every language anyway, which is why the switcher below
  # has always used LOCALES.
  def locales_for_select
    I18n.available_locales.map { |l| [LOCALES[l], l] }
  end

  def locale_switcher
    html = []
    html << '<ul class="navbar-nav text-uppercase">'
    html << '<li class="nav-item dropdown">'
    html << '<a aria-expanded="false" class="nav-link dropdown-toggle" href="#" data-bs-toggle="dropdown" id="dropsownLanguage" role="button">'
    #html << format('%<language>s [%<locale>s]', { language: t("str.language"), locale: I18n.locale })
    html << '<img src="/assets/translate.svg" alt="Image: language icon" class="icon img-fluid" title="Language selection">'
    html << '</a>'
    html << '<ul aria-labelledby="dropdownLanguage" class="dropdown-menu dropdown-menu-lg-end">'
    I18n.available_locales.sort.each do |l|
      html << '<li class="dropdown-item">'
      html << format('<a href="?locale=%<locale>s" class="%<active>s">%<name>s</a>',
                     { active: I18n.locale.eql?(l) ? ' fw-bold' : nil, locale: l, name: LOCALES[l] })
      html << '</li>'
    end
    html << '</ul></li></ul>'
    html.join("\n").html_safe
  end
end
