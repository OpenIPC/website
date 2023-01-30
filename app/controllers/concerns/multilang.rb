# frozen_string_literal: true

module Multilang
  extend ActiveSupport::Concern

  included do
    before_action :set_locale

    helper_method :locales_for_select
    helper_method :locale_switcher
  end

  def self.default_url_options
    { locale: I18n.locale }
  end

  def set_locale
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
    html << '<ul class="navbar-nav text-uppercase"><li class="nav-item dropdown">'
    html << format('<a class="nav-link dropdown-toggle" href="#" data-bs-toggle="dropdown">%<language>s [%<locale>s]</a>', { language: t("str.language"), locale: I18n.locale })
    html << '<ul class="dropdown-menu">'
    I18n.available_locales.sort.each do |l|
      # on = I18n.locale.eql?(l) ? 'active' : nil
      html << format('<li class="dropdown-item"><a href="?locale=%<locale>s">%<name>s</a></li>',
                     { locale: l, name: t("locales.#{l}") })
    end
    html << '</ul></li></ul>'
    html.join("\n").html_safe
  end
end
