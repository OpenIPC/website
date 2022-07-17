class Admin
  class SocsController < AdminController
    def index
      if params[:q]
        @q = params[:q]
        @socs = Soc.left_joins(:vendor).where('model LIKE ?', "%#{@q}%").order(:name, :model)
      else
        @socs = Soc.left_joins(:vendor).order(:name, :model)
      end
      @page_title = "Admin: List of SoCs"
      render "admin/socs/index"
    end

    def show
      @soc = Soc.find(params[:id])
      @page_title = "Admin: SoC #{@soc.full_name}"
      render "admin/socs/show"
    end

    def new
      @soc = Soc.new
      @page_title = "Admin: Adding new SoC"
      render "admin/socs/edit"
    end

    def create
      @soc = Soc.new
      @page_title = "Admin: Creating new SoC"
      if @soc.update(permitted_params)
        redirect_to admin_soc_path(@soc), alert: 'SoC updated.'
      else
        render "admin/socs/edit"
      end
    end

    def edit
      @soc = Soc.find(params[:id])
      @page_title = "Admin: Editing SoC #{@soc.full_name}"
      render "admin/socs/edit"
    end

    def update
      @soc = Soc.find(params[:id])
      @page_title = "Admin: Updating SoC #{@soc.full_name}"
      if @soc.update(permitted_params)
        redirect_to admin_soc_path(@soc), alert: 'SoC updated.'
      else
        render "admin/socs/edit"
      end
    end

    private
      def permitted_params
        params.require(:soc).permit(
          :model, :vendor_id, :version, :status, :load_address, :sdk, :kernel,
          :uboot_filename, :linux_filename, :toolchain_filename, :notes, :build_status_url
        )
      end
  end
end
