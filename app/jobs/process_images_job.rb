# frozen_string_literal: true

class ProcessImagesJob < ApplicationJob
  queue_as :default

  def perform(snapshot)
    [[90, 60], [240, 135], [480, 360], [1920, 1080]].each do |w, h|
      variant = snapshot.file.representation(resize_to_limit: [w, h])
      variant.processed
    end
  end
end
