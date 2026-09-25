# frozen_string_literal: true

require 'test_helper'

# The wall socket pool has to fit a reader with several tabs open.
#
# `limit_conn` counts a connection for its whole life, and a connection in
# `location ^~ /api/v1/wall/cable` lives up to `proxy_read_timeout`, which is an
# hour. So a slot is held for as long as a tab is open -- not for as long as a
# request takes, which is the intuition the rest of this vhost is written
# against.
#
# The first version shipped `limit_conn wall_sockets 4` with the comment
# "nothing legitimate needs four". Production disagreed inside two hours: one
# reader filled the pool with tabs and then spent eight minutes 429'ing their
# own ActionCable reconnects, three a minute, 173 refusals in all. Behind NAT
# the same arithmetic locks out four separate people, and the symptom they see
# is the wall's "frames could not be loaded" message with nothing in it to
# suggest the site did it to them on purpose.
#
# The thing to hold on to, because it is what makes a generous limit safe: this
# pool is not what stops bulk collection. `WallChannel::FRAME_BUDGET` is, it is
# keyed per address, and it is charged per frame -- so opening more sockets buys
# a harvester nothing at all. This limit only bounds nginx's own connection
# slots. Tightening it does not improve the defence; it only breaks readers.
class WallSocketCapacityTest < ActiveSupport::TestCase
  VHOSTS = {
    'production' => 'deploy/nginx/sites-available/org.openipc',
    'dev' => 'deploy/nginx/sites-available/org.openipc.dev'
  }.freeze

  # Four tabs is an ordinary thing for one person to have open, and a NAT can
  # put several people behind one address. Anything at or below that number was
  # the production bug.
  MINIMUM = 8

  VHOSTS.each do |env, path|
    test "the #{env} wall socket pool survives a reader with several tabs" do
      vhost = Rails.root.join(path).read
      limit = vhost[/limit_conn\s+wall_sockets\s+(\d+);/, 1]

      assert limit, "#{path} has no `limit_conn wall_sockets` at all, so a " \
                    'socket flood is bounded only by worker_connections.'

      assert_operator limit.to_i, :>=, MINIMUM, <<~MESSAGE.chomp
        #{path} limits an address to #{limit} concurrent wall sockets.

        A socket here is held for as long as the tab is open (proxy_read_timeout
        is 1h), so this number is a tab count, not a request rate. At #{limit} a
        reader with that many tabs -- or #{limit} people behind one NAT -- fills
        the pool, gets 429 on every reconnect, and sees the wall's failure
        message forever. That is the regression this test exists for.

        If the intent was to tighten the defence against collection, this is the
        wrong lever: WallChannel::FRAME_BUDGET is charged per frame per address,
        so extra sockets win a harvester no extra frames. Lower the budget
        instead and leave this pool generous.
      MESSAGE
    end
  end

  # And the reason a generous pool is safe has to keep being true.
  test 'the frame budget is keyed per address, not per connection' do
    channel = Rails.root.join('app/channels/wall_channel.rb').read

    assert_match(/FRAME_BUDGET/, channel)
    assert_match(/client_ip/, channel,
                 'WallChannel no longer charges its budget against the client ' \
                 'address. If the budget became per-connection, opening more ' \
                 'sockets would multiply what a harvester can take, and the ' \
                 'socket pool above would be load-bearing after all.')
  end
end
