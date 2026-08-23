# frozen_string_literal: true

require 'test_helper'

class SnapshotTest < ActiveSupport::TestCase
  # ProcessImagesJob was enqueued from after_create, which runs inside the
  # transaction. perform_later on the :async adapter hands the job to a thread
  # pool that can reach it before the commit lands, at which point GlobalID
  # cannot find the row and ActiveJob discards the job with a
  # DeserializationError. Production logged those in bursts on the
  # quarter-hour, matching the cameras' cron upload cadence, and the affected
  # snapshots lost their pre-built variants -- the wall generated them on the
  # first page view instead.
  #
  # Asserting on where the callback is registered, rather than driving a real
  # upload, keeps this runnable without libvips and without a fixture image.
  # The `on: :create` scoping is the after_create_commit macro's own job; what
  # is worth pinning is that the enqueue happens after the commit at all.

  test 'image processing is enqueued from a commit callback' do
    after_commit = Snapshot._commit_callbacks.select { |c| c.filter == :process_images }

    assert_equal 1, after_commit.size,
                 'expected exactly one commit callback enqueueing ProcessImagesJob'
  end

  test 'nothing enqueues image processing from inside the transaction' do
    in_transaction = Snapshot._create_callbacks.select { |c| c.filter == :process_images }

    assert_empty in_transaction,
                 'after_create runs before the commit, so the job can outrun the row it references'
  end
end
