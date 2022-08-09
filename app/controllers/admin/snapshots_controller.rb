class Admin
  class SnapshotsController < AdminController
    def destroy
      find_snapshot
      @snapshot.delete
      redirect_to "/open-wall"
    end

    private

      def find_snapshot
        @snapshot = Snapshot.find(params[:id])
      end
  end
end
