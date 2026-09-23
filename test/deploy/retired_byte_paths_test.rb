# frozen_string_literal: true

require 'test_helper'

# The three addresses that used to hand over image bytes, refused at the edge.
#
# Rails stopped routing them on 2026-09-23, which is what actually closes them;
# these nginx rules exist so the eighteen thousand daily requests that will keep
# arriving cost a 410 from nginx instead of a 302 through Ruby, and so a crawler
# is told "gone" rather than "moved".
#
# Order is the whole risk here. nginx takes the FIRST matching regex location,
# and the hexadecimal snapshots block matches `/snapshots/<id>/download` too --
# it is anchored at `^/(?:(?:ru|zh)/)?snapshots/[0-9a-f]+` with no terminator.
# So if these three are ever moved below it they stop applying, silently, with
# nothing failing and nothing in a log to say so. That is what this file
# watches.
class RetiredBytePathsTest < ActiveSupport::TestCase
  VHOST = Rails.root.join('deploy/nginx/sites-available/org.openipc').read.freeze
  DEV_VHOST = Rails.root.join('deploy/nginx/sites-available/org.openipc.dev').read.freeze

  RETIRED = {
    'the original upload' => 'snapshots/[^/]+/download',
    'the unlinked camera.jpg' => 'snapshots/camera',
    'the per-camera JPEG' => 'open-wall/camera/[^/]+\.jpg$'
  }.freeze

  def location_at(suffix, vhost = VHOST)
    vhost.index("location ~ ^/(?:(?:ru|zh)/)?#{suffix}")
  end

  def refuses?(suffix, vhost)
    at = location_at(suffix, vhost)
    return false unless at

    vhost[at..].split("\n    }").first.include?('return 410')
  end

  RETIRED.each do |what, suffix|
    test "#{what} is answered 410 at the edge" do
      at = location_at(suffix)
      assert at, "no nginx location for #{what}"

      block = VHOST[at..].split("\n    }").first
      assert_includes block, 'return 410', "#{what} has a location but does not refuse"
    end
  end

  # The ordering invariant, stated as the thing that would break.
  test 'every retired path is matched before the hexadecimal snapshots block' do
    hex = location_at('snapshots/[0-9a-f]+')
    assert hex, 'the hexadecimal snapshots location has moved or been renamed'

    RETIRED.each do |what, suffix|
      at = location_at(suffix)
      assert at, "no nginx location for #{what}"

      assert_operator at, :<, hex, <<~MESSAGE.chomp
        The rule for #{what} now sits BELOW the hexadecimal snapshots location.
        nginx takes the first matching regex, and that block matches these
        addresses too -- so this rule no longer applies to anything. Nothing
        else will fail; the requests simply start being proxied again.
      MESSAGE
    end
  end

  # Dev must refuse them the same way, and this is not tidiness.
  #
  # Dev answered these 302 through the catch-all while production answered 410,
  # which makes the environment a worse rehearsal than it looks: a validation
  # run there would have reported a status production never returns. Found by
  # curling both after the deploy, not by reading either file.
  RETIRED.each do |what, suffix|
    test "dev refuses #{what} exactly as production does" do
      # Not merely "a location exists". The first version of this asserted the
      # prefix was present and nothing else, so dev could have answered 302,
      # 404 or anything at all and still passed -- which is the same shape of
      # hole that let dev and production disagree in the first place.
      assert refuses?(suffix, DEV_VHOST),
             "org.openipc.dev does not answer 410 for #{what}, so dev and production disagree"
    end
  end

  # Ordering matters on dev for the same reason it does on production: the
  # hexadecimal snapshots location matches /snapshots/<id>/download too.
  # No skip: a skipped test is one nobody reads the day it starts mattering.
  # Dev has no hexadecimal snapshots location today -- its snapshot pages go
  # through `location /` to @rails -- so there is nothing to be shadowed BY,
  # and that is asserted rather than assumed. Add one and this starts checking
  # the order, which is when it needs to.
  test 'dev matches every retired path before its hexadecimal snapshots block' do
    hex = location_at('snapshots/[0-9a-f]+', DEV_VHOST)

    if hex.nil?
      assert_nil hex, 'dev has no hexadecimal snapshots location, so nothing can shadow the rules'
      next
    end

    RETIRED.each do |what, suffix|
      at = location_at(suffix, DEV_VHOST)

      assert at, "org.openipc.dev has no rule for #{what}"
      assert_operator at, :<, hex,
                      "on dev the rule for #{what} sits below the hexadecimal block and never applies"
    end
  end

  # Rails must not carry the actions either. The edge rule is the cheap answer,
  # not the guarantee -- dev, the test suite and any direct hit on the container
  # never pass through nginx at all.
  #
  # Asserted against the route TABLE rather than by recognising paths, because
  # recognition is misleading here: `/snapshots/camera.jpg` still resolves, to
  # `snapshots#show` with id "camera", and is refused a moment later when no
  # snapshot has that public_id. What must be true is narrower and exact --
  # nothing anywhere maps to a snapshots action that sends a file.
  test 'no route maps to a snapshots action that serves bytes' do
    offenders = Rails.application.routes.routes.filter_map do |route|
      defaults = route.defaults
      next unless defaults[:controller] == 'snapshots'
      next unless %w[download].include?(defaults[:action].to_s)

      "#{defaults[:action]} at #{route.path.spec}"
    end

    assert_empty offenders, "these routes reach a byte-serving action: #{offenders.join(', ')}"
  end

  # And the controller must not still define them, since a route is one line
  # away from coming back.
  test 'the controller defines no byte-serving action' do
    source = Rails.root.join('app/controllers/snapshots_controller.rb').read

    ['def download', 'def send_blob', 'def send_camera_jpeg'].each do |definition|
      assert_not_includes source, definition,
                          "SnapshotsController still defines #{definition}"
    end
  end
end
