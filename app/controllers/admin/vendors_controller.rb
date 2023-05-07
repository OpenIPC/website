class Admin
  class VendorsController < AdminController
    def index
      @vendors = Vendor.order(:name)
      @page_title = 'List of Vendors'
      render 'admin/vendors/index'
    end

    def show
      @vendor = Vendor.find(params[:id])
      @page_title = @vendor.name
      render 'admin/vendors/show'
    end

    def new
      @vendor = Vendor.new
      @page_title = "Adding new Vendor"
      render 'admin/vendors/edit'
    end

    def create
      @vendor = Vendor.new
      if @vendor.update(permitted_params)
        redirect_to admin_vendor_path(@vendor), notice: 'Vendor updated.'
      else
        @page_title = 'Creating new Vendor'
        render 'admin/vendors/edit'
      end
    end

    def edit
      @vendor = Vendor.find(params[:id])
      @page_title = "Editing #{@vendor.full_name}"
      render 'admin/vendors/edit'
    end

    def update
      @vendor = Vendor.find(params[:id])
      if @vendor.update(permitted_params)
        redirect_to admin_vendor_path(@vendor), notice: 'Vendor updated.'
      else
        @page_title = "Updating #{@vendor.full_name}"
        render 'admin/vendors/edit'
      end
    end

    private

    def permitted_params
      params.require(:vendor).permit(:name, :full_name, :urlname, :website_url)
    end
  end
end
