# frozen_string_literal: true

require 'test_helper'

# deploy/memory-probe.sh compares two images under a fixed load, and its
# results are only as good as its path list. /supported-hardware sat in that
# list for a while: it is a 301 to /supported-hardware/featured, which is also
# in the list, so a sixth of every run was a redirect that reached no
# controller, counted towards throughput, and was then dropped from the latency
# sample for not being a 200.
#
# curl is deliberately not given -L, so the same mistake would be silent again.
# This is what notices.
class MemoryProbeTest < ActiveSupport::TestCase
  PROBE = Rails.root.join('deploy/memory-probe.sh').read.freeze

  # The paths=( ... ) array, which is the only thing here worth asserting on.
  def probe_paths
    PROBE[/^paths=\((.*?)\)$/m, 1].to_s.split.grep(%r{\A/})
  end

  test 'the probe has a path list' do
    assert_operator probe_paths.size, :>=, 4,
                    'a load of one or two URLs is not the site and will not fragment like it'
  end

  # A redirect and an unrouted path look the same from here: this app's
  # catch-all answers both with application#route_not_found, and neither
  # reaches the controller the probe is supposed to be loading.
  test 'every path the probe loads reaches a controller' do
    dead = probe_paths.reject do |path|
      route = Rails.application.routes.recognize_path(path, method: :get)
      route[:action].to_s != 'route_not_found'
    rescue ActionController::RoutingError
      false
    end

    assert_empty dead, <<~MESSAGE.chomp
      These probe paths do not reach a controller:

      #{dead.map { |p| "  #{p}" }.join("\n")}

      Either they redirect or this app does not route them. Either way they
      cost a request, exercise nothing, and are then dropped from the latency
      sample for not being 200s -- so they weaken the load and overstate the
      throughput at the same time. /supported-hardware was in this list and is
      a 301 to /supported-hardware/featured, which was in it too.

      Point them at the page they redirect to, or drop them.
    MESSAGE
  end

  # Checked once per cycle, a worker that reaches its deadline on the first of
  # six paths still makes the other five, each able to wait out the timeout --
  # so a run stretches exactly when the server is slow, which is when
  # comparability matters most.
  test 'the probe checks its deadline before every request' do
    body = PROBE[/^worker\(\) \{(.*?)^\}/m, 1].to_s

    assert_match(/for p in .*\n\s*\[ "\$\(date \+%s\)" -lt "\$deadline" \]/, body,
                 'the deadline check belongs inside the path loop, not only around it')
  end

  test 'throughput is divided by the time that elapsed, not the time requested' do
    assert_match(/elapsed=\$\(\( \$\(date \+%s\) - started \)\)/, PROBE)
    assert_match(/echo "\$requests \$elapsed"/, PROBE,
                 'an in-flight request can outlive the deadline; dividing by the ' \
                 'requested duration would report a run that overran as faster than it was')
  end
end
