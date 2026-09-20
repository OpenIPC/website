# frozen_string_literal: true

module BootstrapHelper

  # Both of these take a literal path, and both are how most of the navigation
  # is written -- so prefixing here covers the navbar and the dropdowns in one
  # place rather than in thirty templates. locale_path leaves an English path
  # and an external URL alone (#154).
  def navbar_link(name, url)
    content_tag :li, link_to(name, locale_path(url), class: 'nav-link'), class: 'nav-item'
  end

  def dropdown_link(name, url)
    content_tag :li, link_to(name, locale_path(url), class: 'dropdown-item')
  end

  def tab_lap(tab_name, tab_text, active = nil)
    content_tag :li, button_tag(tab_text, type: 'button', role: 'tab', id: "##{tab_name}-tab", class: "nav-link #{active}",
                                data: { 'bs-toggle': 'tab', 'bs-target': "##{tab_name}-tab-pane" },
                                aria: { controls: "#{tab_name}-tab-pane", selected: active.nil? ? 'false' : 'true' }),
                class: 'nav-item', role: 'presentation'
  end
end
