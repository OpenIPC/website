# frozen_string_literal: true

require 'test_helper'

# The frame budget has to be judged against two things, and the first version
# only checked one.
#
# It was sized from a reader: a thorough visit is about 223 frames, so the
# ceiling went an order of magnitude above at 3,000 and that looked generous.
# Nobody asked how big the wall is. It holds roughly 2,767 frames across 20
# cameras, so the budget was LARGER THAN EVERYTHING IT PROTECTED -- one
# address could take every frame that exists in an hour and stay inside the
# limit. The number looked careful and bounded nothing.
#
# So this file pins both comparisons. A budget that drifts up until it exceeds
# the corpus is not a budget, and a budget that drifts down into a reader's
# visit stops the pictures for somebody behind a carrier NAT.
class WallBudgetSizingTest < ActiveSupport::TestCase
  # What the heaviest genuine visit costs: the gallery, a camera page, its
  # archive strip and its slideshow.
  THOROUGH_VISIT = 18 + 13 + 96 + 96

  # What the wall actually holds. Measured on production 2026-09-24; it grows
  # by about 1,343 a day and is purged after two days, so it is stable within
  # a factor well under the margins below.
  CORPUS = 2_767

  test 'the budget is well clear of a thorough reading session' do
    assert_operator WallChannel::FRAME_BUDGET, :>, THOROUGH_VISIT * 4, <<~MESSAGE.chomp
      The budget (#{WallChannel::FRAME_BUDGET}) is close to what one thorough
      visit costs (#{THOROUGH_VISIT}). Carrier-grade NAT puts many readers
      behind one address and this site has a large audience behind exactly
      that, so a ceiling near a single visit stops real people seeing
      pictures -- the one failure this work must not cause.
    MESSAGE
  end

  test 'the budget is a fraction of the wall, not larger than it' do
    assert_operator WallChannel::FRAME_BUDGET, :<, CORPUS / 2, <<~MESSAGE.chomp
      The budget (#{WallChannel::FRAME_BUDGET}) lets one address take more
      than half of the entire wall (#{CORPUS} frames) in an hour.

      This is the comparison the first version missed. A budget reasoned only
      against a reader looks generous at any size; measured against the corpus,
      3,000 meant a single address could sweep everything that exists inside
      the hour and never be refused.

      Note the two bounds in this file pull against each other, and with a
      corpus this small they nearly meet: a thorough visit is 223 frames and
      the wall holds 2,767. There is no per-address number that satisfies both
      comfortably. That is a fact about this instrument, not a target to tune
      towards -- the page grant is what stops a distributed fleet.
    MESSAGE
  end
end
