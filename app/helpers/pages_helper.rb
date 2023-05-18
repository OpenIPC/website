# frozen_string_literal: true

module PagesHelper
  def page_title
    [@page_title, 'OpenIPC'].join(' - ')
  end
end
