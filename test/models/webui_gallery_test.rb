# frozen_string_literal: true

require 'test_helper'

# The manifest is read by two things that never run together: the page, and
# tools/webui-gallery, which photographs a camera. Nothing else would notice a
# manifest that names a screenshot nobody took, or a slug that appears twice --
# the page would simply render a broken image.
class WebuiGalleryTest < ActiveSupport::TestCase
  IMAGES = Rails.root.join('app/assets/images/webui')

  test 'every screen it names has both of its images' do
    WebuiGallery.screens.each do |screen|
      %W[#{screen.slug}.webp #{screen.slug}-thumb.webp].each do |file|
        assert_path_exists IMAGES.join(file),
                           "the manifest lists #{screen.slug} but #{file} was never captured"
      end
    end
  end

  test 'it leaves no image behind that nothing displays' do
    # A full run of the tool deletes these. One left here is a page that was
    # dropped from the manifest by hand, and it costs the repository a file
    # nobody will ever look at again.
    on_disk = Dir.children(IMAGES).map { |f| f.sub(/(-thumb)?\.webp\z/, '') }.uniq
    assert_equal WebuiGallery.slugs.sort, on_disk.sort
  end

  test 'it does not name a page that acts on being rendered' do
    # fw-reset.cgi runs sysupgrade -s -n -x --web as it loads, wiping the
    # overlay and rebooting; fw-restart.cgi reboots the same way. The tool
    # refuses to open them, and this stops them reaching the manifest in the
    # first place, where the refusal would only be discovered mid-run.
    destructive = WebuiGallery.screens.map(&:cgi) & %w[fw-reset.cgi fw-restart.cgi]
    assert_empty destructive, "the manifest asks the tool to open #{destructive.join(', ')}"
  end

  test 'each screen is described completely' do
    slugs = WebuiGallery.slugs
    assert_equal slugs.uniq, slugs, 'two screens share a slug and would overwrite each other'

    WebuiGallery.screens.each do |screen|
      assert_match(/\A[a-z0-9-]+\z/, screen.slug, 'a slug becomes a filename')
      assert_predicate screen.caption.to_s, :present?
      assert_match(/\A[a-z0-9-]+\.cgi\z/, screen.cgi.to_s)
      assert_kind_of Integer, screen.settle
      assert_operator screen.settle, :>, 0
      # A misspelt or truthy-but-not-true value would read as "no live player
      # here" and the tool would photograph the camera's own view.
      assert_includes [nil, true], screen.scene, "#{screen.slug}: scene must be true or absent"
    end
  end

  test 'the pages that show live video are marked as such' do
    # The mark is what makes the tool fail rather than publish the camera's own
    # view when it cannot find a player to cover. Losing it is silent, so it is
    # asserted here against the pages that are known to carry one.
    %w[preview majestic-settings].each do |slug|
      screen = WebuiGallery.screens.find { |s| s.slug == slug }
      next if screen.nil?

      assert_predicate screen, :scene?, "#{slug} shows a live player and must be marked scene: true"
    end
  end
end
