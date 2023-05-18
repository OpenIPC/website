# frozen_string_literal: true

class Admin
  class SnapshotsController < AdminController
    def destroy
      find_snapshot
      case params[:scope]
      when 'mac'
        Snapshot.where(mac_address: @snapshot.mac_address).delete_all
      else
        @snapshot.delete
      end
      redirect_to '/open-wall'
    end

    private

    def find_snapshot
      @snapshot = Snapshot.find(params[:id])
    end
  end
end
