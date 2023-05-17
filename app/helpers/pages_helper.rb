module PagesHelper
  def page_title
    [@page_title, 'OpenIPC'].join(' - ')
  end
end
