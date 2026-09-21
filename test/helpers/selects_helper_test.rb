# frozen_string_literal: true

require 'test_helper'

class SelectsHelperTest < ActionView::TestCase
  # The admin's segment select is bound to a nullable column, and 59 of the
  # catalogue's chips are null. A select with no blank option shows its first
  # entry selected, so opening any of those chips to fix a typo in `notes` and
  # pressing Save would have declared it a drone part -- changing the business
  # wording and the event name on the public page, from an edit that never
  # touched the field.
  test 'the segment select offers the unclassified state, first' do
    options = list_of_segments_for_select

    assert_equal '', options.first.last, 'no blank option, so null renders as the first segment'
    assert_equal Soc::SEGMENTS, options.drop(1).map(&:last)
  end

  # Rendered, not just listed: `selected` on the blank entry is the part that
  # decides what a save writes.
  test 'a chip nobody has classified renders with nothing selected' do
    soc = Soc.new(segment: nil)
    html = select('soc', :segment, list_of_segments_for_select, { selected: soc.segment })

    assert_no_match(/<option[^>]*selected[^>]*value="fpv"/, html)
  end
end
