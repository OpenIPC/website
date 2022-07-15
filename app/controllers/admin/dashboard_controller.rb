class Admin
  class DashboardController < AdminController
    def index
      @page_title = "Admin Dashboard"
      render "admin/dashboard/show"
    end
  end
end
