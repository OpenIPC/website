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

    def update
      @soc = Soc.find(params[:id])
      if @soc.update(permitted_params)
        redirect_to admin_socs_path, alert: 'SoC updated.'
      else
        render "admin/socs/edit"
      end
    end

    private
      def permitted_params
        params.require(:soc).permit(:model, :vendor_id, :version, :status,
                                    :load_address, :sdk, :kernel, :uboot_filename, :linux_filename, :notes)
      end
  end
end
