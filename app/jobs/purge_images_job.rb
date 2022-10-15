# frozen_string_literal: true

class PurgeImagesJob < ApplicationJob
  queue_as :default

  def perform
    Snapshot.where('created_at < ?', 2.days.ago).each do |s|
      s.file.purge_later
      s.delete
    end
  end
end
