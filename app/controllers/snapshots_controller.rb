class SnapshotsController < ApplicationController
  skip_before_action :verify_authenticity_token

  def index
    page = params[:page] || 1
    @snapshots = Snapshot.where('created_at > ?', 1.day.ago).group(:mac_address).order(created_at: :desc).page(page).per(9)
    @page_title = "Open Wall, page #{page}"
    render "snapshots/index"
  end

  def create
    @snapshot = Snapshot.new
    @snapshot.ip_address = request.remote_ip
    if @snapshot.update(permitted_params)
      head :created, location: snapshot_path(@snapshot)
    else
      puts @snapshot.errors.inspect
      head :too_many_requests, retry_after: Snapshot::INTERVAL_LIMIT
    end
  end

  def show
    @snapshot = Snapshot.find(params[:id])
    @snapshots = Snapshot.where(mac_address: @snapshot.mac_address).order(created_at: :desc).page(params[:page])
    @page_title = "Open Wall, image ##{params[:id]}"
    render "snapshots/show"
  end

  private
    def permitted_params
      params.permit(:file, :flash_size, :mac_address, :hostname, :soc, :sensor, :firmware, :uptime, :soc_temperature)
    end
end
