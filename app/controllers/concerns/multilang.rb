# frozen_string_literal: true

module Multilang
  extend ActiveSupport::Concern

  LOCALES = {
    de: 'Deutsch',
    en: 'English',
    es: 'Español',
    fa: 'الفارسية',
    fr: 'Français',
    it: 'Italiano',
    ja: '日本語',
    pl: 'Polska',
    # pt: 'Português',
    ru: 'Русский',
    zh: '中文'
  }

  included do
    before_action :set_locale

    helper_method :browser_locale
    helper_method :locales_for_select
    helper_method :locale_switcher
  end

  def browser_locale
    locales = request.env['HTTP_ACCEPT_LANGUAGE'] || ''
    locales.scan(/[a-z]{2}(?=;)/).find do |locale|
      I18n.available_locales.include?(locale.to_sym)
    end
  end

  def self.default_url_options
    { locale: I18n.locale }
  end

  def set_locale
    # detect locale from browser languages
    session[:locale] ||= browser_locale

    # save session locale value to default if not valid
    old_locale = session[:locale].to_s.to_sym
    unless I18n.available_locales.include?(old_locale)
      session[:locale] = I18n.default_locale
    end

    # save session locale value from params if valid
    if params[:locale]
      new_locale = params[:locale].to_s.to_sym
      if I18n.available_locales.include?(new_locale)
        session[:locale] = new_locale
      end
    end

    # set app locale from session
    I18n.locale = session[:locale]
  end

  def locales_for_select
    I18n.available_locales.map { |l| [t("locales.#{l}"), l] }
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
