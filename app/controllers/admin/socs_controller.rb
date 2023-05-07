class Admin
  class SocsController < AdminController
    def index
      if params[:q]
        @q = params[:q]
        @socs = Soc.left_joins(:vendor).where('model LIKE ?', "%#{@q}%").order(:name, :model)
      else
        @socs = Soc.left_joins(:vendor).order(:name, :model)
      end
      @page_title = "List of SoCs"
      render "admin/socs/index"
    end

    def show
      @soc = Soc.find(params[:id])
      @page_title = "SoC #{@soc.full_name}"
      render "admin/socs/show"
    end

    def new
      if params[:from]
        _soc = Soc.find(params[:from])
        @soc = _soc.dup
        @page_title = "Clonning SoC #{_soc.full_name}"
      else
        @soc = Soc.new
        @page_title = "Adding new SoC"
      end
      render "admin/socs/edit"
    end

    def create
      @soc = Soc.new
      if @soc.update(permitted_params)
        redirect_to admin_soc_path(@soc), notice: 'SoC updated.'
      else
        @page_title = "Creating new SoC"
        render "admin/socs/edit"
      end
    end

    def edit
      @soc = Soc.find(params[:id])
      @page_title = "Editing SoC #{@soc.full_name}"
      render "admin/socs/edit"
    end

    def update
      @soc = Soc.find(params[:id])
      if @soc.update(permitted_params)
        redirect_to admin_soc_path(@soc), notice: 'SoC updated.'
      else
        @page_title = "Updating SoC #{@soc.full_name}"
        render "admin/socs/edit"
      end
    end

    private

    def permitted_params
      params.require(:soc).permit(
        :model, :family, :vendor_id, :version, :status, :load_address, :sdk, :kernel,
        :uboot_filename, :linux_filename, :toolchain_filename, :notes, :build_status_url, :featured
      )
    end
  end
end
