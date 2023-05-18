# frozen_string_literal: true

module BinariesHelper
  def css_highlight(b)
    free = b[:free]
    full = b[:size]
    css = []
    if free.negative?
      css << "oversize"
    elsif free < full / 10.0
      css << "bloated"
    else
      css << "slim"
    end
    css.join(" ")
  end
end
