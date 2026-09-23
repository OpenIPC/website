# frozen_string_literal: true

require 'test_helper'

# Wall frames are drawn on a <canvas>, so no stylesheet may size one by naming
# `img` alone.
#
# This exists because of a live regression. The homepage mosaic sized its tiles
# with `.wall-tile img { aspect-ratio: 16/9; object-fit: cover; height: 100% }`,
# and when frames moved onto the wall channel those tiles became canvases. The
# selector stopped matching, an unstyled canvas draws at its own bitmap size,
# and every landscape frame sat at the top of a tall dark tile. Nothing failed:
# not the suite, not nginx, not the browser check, which asked whether frames
# PAINTED and they did. It was reported by someone looking at the site.
#
# The general shape is worth naming, because it will happen again to whoever
# converts the next surface: changing the ELEMENT silently drops every
# element-name selector aimed at it, and CSS has no way to complain.
class WallSurfaceStylingTest < ActiveSupport::TestCase
  STYLESHEETS = Rails.root.glob('app/assets/stylesheets/**/*.scss').freeze

  # Blocks that style something on a wall surface. Keyed by the selector that
  # opens them, since that is what a reader greps for.
  WALL_BLOCKS = ['.wall-mosaic'].freeze

  WALL_BLOCKS.each do |selector|
    test "#{selector} sizes canvases, not only images" do
      file = STYLESHEETS.find { |f| f.read.include?(selector) }
      assert file, "no stylesheet contains #{selector}"

      block = file.read[/#{Regexp.escape(selector)}\s*\{.*?\n\}/m]
      assert block, "could not read the #{selector} block"

      image_rules = block.scan(/^\s*([\w\s,]*\bimg\b[\w\s,]*)\s*\{/).flatten
      image_rules.each do |rule|
        assert_includes rule, 'canvas', <<~MESSAGE.chomp
          `#{rule.strip}` inside #{selector} names img and not canvas.

          Wall frames are canvases since 2026-09-23. A selector that names only
          the element stops matching the day the element changes, silently --
          which is exactly how the homepage mosaic ended up drawing landscape
          frames in tall portrait tiles on production.
        MESSAGE
      end
    end
  end

  # And the markup really does use canvases, so the rule above is not guarding
  # a surface that quietly went back to images.
  test 'the homepage mosaic renders canvases' do
    home = Rails.root.join('app/views/pages/home.html.erb').read

    assert_includes home, 'wall_frame_tag'
    assert_not_includes home, 'image_tag snapshot'
  end
end
