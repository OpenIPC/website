# frozen_string_literal: true

class ProcessImagesJob < ApplicationJob
  queue_as :default

  def perform(snapshot)
    # Pre-process the named variants the wall actually renders. This also forces
    # HEIF uploads to be decoded once here, in the background, instead of on the
    # first page view.
    %i[icon icon2 thumb fullhd].each do |name|
      snapshot.file.variant(name).processed
    end
  end
end
