class SnapshotsController < ApplicationController
  skip_before_action :verify_authenticity_token

  def index
    page = params[:page] || 1
    sql = "SELECT s1.* FROM snapshots s1 LEFT JOIN snapshots s2" \
      " ON (s1.mac_address = s2.mac_address AND s1.created_at < s2.created_at)" \
      " WHERE s2.id IS NULL AND s1.created_at > SUBDATE(NOW(), INTERVAL 1 DAY)" \
      " ORDER BY created_at DESC"
    @snapshots = Kaminari.paginate_array(Snapshot.find_by_sql(sql)).page(page).per(12)
    @page_title = "Open Wall, page #{page}"
    render "snapshots/index"
  end

  def create
    @snapshot = Snapshot.new
    @snapshot.ip_address = request.remote_ip
    if @snapshot.update(permitted_params)
      head :created, location: snapshot_path(@snapshot)
    else
      s = Snapshot.select(:created_at).where(mac_address: @snapshot.mac_address).order(:created_at).last
      head :too_many_requests, retry_after: (Snapshot::INTERVAL_LIMIT - (Time.now - s.created_at)).to_i
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
