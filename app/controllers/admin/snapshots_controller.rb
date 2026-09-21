# frozen_string_literal: true

class Admin
  class SnapshotsController < AdminController
    def destroy
      find_snapshot
      # destroy, never delete: has_one_attached relies on the destroy callback
      # to purge the blob. delete/delete_all skip it and strand the blob, its
      # variant blobs and every file on disk, unreachable by any code path.
      case params[:scope]
      when 'mac'
        Snapshot.where(mac_address: @snapshot.mac_address).find_each(&:destroy)
      else
        @snapshot.destroy
      end
      redirect_to '/open-wall'
    end

    private

    # The admin views build their links from to_param like every other view,
    # so they carry a public_id too.
    def find_snapshot
      @snapshot = Snapshot.find_by!(public_id: params[:id])
    end
  end
end
