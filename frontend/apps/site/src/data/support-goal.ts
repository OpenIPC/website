/**
 * What the donate page asks people to help reach (#198, baked for #160).
 *
 * data/support_goal.yml is the source; this is the copy the prerendered
 * pages read, because the build has no Ruby. test/deploy/static_bundle_test.rb
 * fails when the two disagree, so raising the goal is still one edit to the
 * YAML and a red test until this follows it.
 *
 * A setting, not a computation: "next round number above the current count"
 * would move the goal every time somebody joined, which is the one thing a
 * goal must not do.
 */
export const SUPPORT_GOAL = 75;
