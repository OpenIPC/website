ENV["RAILS_ENV"] ||= "test"
require_relative "../config/environment"
require "rails/test_help"

class ActiveSupport::TestCase
  # Run tests in parallel with specified workers
  parallelize(workers: :number_of_processors)

  # Each process gets its own WallImage.root (see the comment there), so the
  # run sweeps them by pattern rather than by name: a forked worker exits
  # without running after_run, so only the parent is reliably here at the end,
  # and it does not know the children's pids. Without this the directories
  # accumulate under tmp/ one per worker per run -- 24 after eight runs.
  Minitest.after_run { FileUtils.rm_rf(Dir.glob(Rails.root.join('tmp', 'wall-test-*'))) }

  # Setup all fixtures in test/fixtures/*.yml for all tests in alphabetical order.
  fixtures :all

  # Add more helper methods to be used by all tests here...
end
