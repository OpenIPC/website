class SocsController < ApplicationController
  def index
    @_vendor = params[:vendor] if params[:vendor]
    if @_vendor.in?(Vendor.all.map(&:name))
      @socs = Soc.left_joins(:vendor).where(vendors:{ name: @_vendor }).order(:model)
      @page_title = "Filtered by #{@_vendor}"
    else
      @socs = Soc.left_joins(:vendor).order(:name, :model)
      @page_title = "Full List"
    end
    render "socs/index"
  end

  def show
    @soc = Soc.find(params[:id])
    render "socs/show"
  end

  def update
    @soc = Soc.find(params[:id])
    @ipaddr = permitted_params[:ipaddr]
    @serverip = permitted_params[:serverip]
    @flash_type = permitted_params[:flash_type]
    if @flash_type.eql?("nor16m")
      @flash_size_hex = "0x1000000" # 16M
    else
      @flash_size_hex = "0x800000" # 8M
    end
    render "socs/update", status: :unprocessable_entity
  end

  def legend
    render "socs/legend"
  end

  private
    def permitted_params
      params.require(:camera).permit(:ipaddr, :serverip, :flash_size, :flash_type)
    end
end
