module BootstrapHelper

  def navbar_link(name, url)
    content_tag :li, link_to(name, url, class: 'nav-link'), class: 'nav-item'
  end

  def dropdown_link(name, url)
    content_tag :li, link_to(name, url, class: 'dropdown-item')
  end

  def tab_lap(tab_name, tab_text, active = nil)
    content_tag :li, button_tag(tab_text, type: 'button', role: 'tab', id: "##{tab_name}-tab", class: "nav-link #{active}",
                                data: { "bs-toggle": "tab", "bs-target": "##{tab_name}-tab-pane" },
                                aria: { controls: "#{tab_name}-tab-pane", selected: active.nil? ? "false" : "true" }),
                class: "nav-item", role: "presentation"

    # raw "<li class=\"nav-item\" role=\"presentation\">" \
    #   "<button role=\"tab\" id=\"##{tab_name}-tab\" class=\"nav-link #{active}\" data-bs-toggle=\"tab\"" \
    #   " data-bs-target=\"##{tab_name}-tab-pane\" aria-controls=\"#{tab_name}-tab-pane\" aria-selected=\"#{s}\">#{tab_text}</button>" \
    #   "</li>"
  end
end
