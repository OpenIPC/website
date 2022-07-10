module BootstrapHelper
  def tab_lap(tab_name, tab_text, active = nil)
    s = active.nil? ? "false" : "true"
    raw "<li class=\"nav-item\" role=\"presentation\">" \
      "<button role=\"tab\" id=\"##{tab_name}-tab\" class=\"nav-link #{active}\" data-bs-toggle=\"tab\"" \
      " data-bs-target=\"##{tab_name}-tab-pane\" aria-controls=\"#{tab_name}-tab-pane\" aria-selected=\"#{s}\">#{tab_text}</button>" \
      "</li>"
  end
end
