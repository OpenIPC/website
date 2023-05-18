# frozen_string_literal: true

module SocsHelper
  def na
    content_tag(:span, 'n/a', class: 'text-black-50')
  end

  def value_or_na(text)
    text.blank? ? na : text
  end
end
