# frozen_string_literal: true

class Admin
  class DashboardController < AdminController
    def index
      @page_title = t('pages.admin.dashboard.title')
      render 'admin/dashboard/show'
    end
  end
end
