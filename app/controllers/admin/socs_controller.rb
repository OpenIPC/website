class Admin
  class SocsController < AdminController
    def index
      @socs = Soc.all
      @page_title = "Admin: List of SoCs"
      render "admin/socs/index"
    end

    def show
      @soc = Soc.find(params[:id])
      @page_title = "Admin: SoC #{@soc.full_name}"
      render "admin/socs/show"
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
        redirect_to admin_socs_path, alert: 'SoC updated.'
      else
        render "admin/socs/edit"
      end
    end

    private
      def permitted_params
        params.require(:soc).permit(
          :model, :vendor_id, :version, :status,
          :load_address, :sdk, :kernel, :uboot_filename,
          :linux_filename, :notes
        )
      end
  end
end
