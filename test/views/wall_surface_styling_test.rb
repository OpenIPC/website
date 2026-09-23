# frozen_string_literal: true

require 'test_helper'

# Wall frames are drawn on a <canvas>, so no stylesheet may size one by naming
# `img` alone — and the rule that does size them has to keep existing.
#
# This exists because of a live regression. The homepage mosaic sized its tiles
# with `.wall-tile img { aspect-ratio: 16/9; object-fit: cover; height: 100% }`,
# and when frames moved onto the wall channel those tiles became canvases. The
# selector names an ELEMENT, so it silently stopped matching, an unstyled
# canvas draws at its own bitmap size, and every landscape frame sat at the top
# of a tall dark tile. Nothing failed: not the suite, not nginx, not the browser
# check, which asked whether frames PAINTED and they did. It was reported by
# someone looking at the site.
#
# The general shape is worth naming, because it will happen again to whoever
# converts the next surface: changing the ELEMENT silently drops every
# element-name selector aimed at it, and CSS has no way to complain.
class WallSurfaceStylingTest < ActiveSupport::TestCase
  STYLESHEETS = Rails.root.glob('app/assets/stylesheets/**/*.scss').freeze

  # Blocks that style something on a wall surface, and what a tile needs for
  # the crop to hold. Keyed by the selector that opens the block, since that is
  # what a reader greps for.
  WALL_BLOCKS = {
    '.wall-mosaic' => %w[aspect-ratio object-fit height width]
  }.freeze

  # Any selector naming img, with the punctuation selectors actually contain.
  #
  # The first version of this allowed only word characters, whitespace and
  # commas, so `.wall-tile img {` did not match -- the exact selector named in
  # the comment above, and the exact one that broke. A test that cannot see the
  # bug it was written for is worse than none, because it reads as cover.
  RULE_HEADER = /^[ \t]*([^{}\n;]*?)\s*\{/
  NAMES_IMG = /(?<![\w-])img(?![\w-])/
  NAMES_CANVAS = /(?<![\w-])canvas(?![\w-])/

  def image_only_message(header, selector)
    <<~MESSAGE.chomp
      `#{header.strip}` inside #{selector} names img and not canvas.

      Wall frames are canvases since 2026-09-23. A selector that names only the
      element stops matching the day the element changes, silently -- which is
      exactly how the homepage mosaic ended up drawing landscape frames in tall
      portrait tiles on production.
    MESSAGE
  end

  def block_for(selector)
    file = STYLESHEETS.find { |f| f.read.include?(selector) }
    assert file, "no stylesheet contains #{selector}"

    block = file.read[/#{Regexp.escape(selector)}\s*\{.*?\n\}/m]
    assert block, "could not read the #{selector} block"
    block
  end

  WALL_BLOCKS.each do |selector, required|
    test "#{selector} sizes canvases, not only images" do
      headers = block_for(selector).scan(RULE_HEADER).flatten

      headers.select { |h| h.match?(NAMES_IMG) }.each do |header|
        assert_match(NAMES_CANVAS, header, image_only_message(header, selector))
      end
    end

    # Naming canvas is not enough on its own: deleting the rule, or gutting its
    # declarations, leaves the canvases unconstrained again and the check above
    # has nothing to iterate. Assert the sizing exists and still says what makes
    # the crop work.
    test "#{selector} keeps a sizing rule that constrains a canvas" do
      block = block_for(selector)
      sizing = block[/^[ \t]*[^{}\n;]*(?<![\w-])canvas(?![\w-])[^{}\n;]*\{(.*?)\n[ \t]*\}/m, 1]

      assert sizing, <<~MESSAGE.chomp
        #{selector} has no rule naming canvas at all, so nothing sizes the
        frames and each one draws at its own bitmap size inside a tile whose
        height comes from the grid. That is the production regression this file
        exists for, not a tidiness point.
      MESSAGE

      required.each do |property|
        assert_includes sizing, property,
                        "The canvas rule inside #{selector} no longer sets `#{property}`. " \
                        "Without all of #{required.join(', ')} the tile stops cropping to " \
                        "its own shape and starts taking the frame's."
      end
    end
  end

  # And the markup really does use canvases, so the rules above are not
  # guarding a surface that quietly went back to images.
  test 'the homepage mosaic renders canvases' do
    home = Rails.root.join('app/views/pages/home.html.erb').read

    assert_includes home, 'wall_frame_tag'
    assert_not_includes home, 'image_tag snapshot'
  end
end
