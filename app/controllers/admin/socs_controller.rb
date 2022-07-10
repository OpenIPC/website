class Admin
  class SocsController < AdminController
    def index
      @socs = Soc.all
      render "admin/socs/index"
    end

    def show
      @soc = Soc.find(params[:id])
      render "admin/socs/show"
    end

    def edit
      @soc = Soc.find(params[:id])
      render "admin/socs/edit"
    end
  end
end
