# frozen_string_literal: true

require 'test_helper'

# /up is the endpoint a deploy trusts. `deploy/deploy.sh` waits for it and
# reverts the release if it does not answer within 90s, the container
# healthcheck in `deploy/docker-compose.yml` is what makes `docker ps` say
# healthy, and `openipc-deploy status` reports up or DOWN from it. It is a
# rack proc in config/routes.rb answering "ok" -- no controller, no view, no
# database -- which is why it can be asked every few seconds.
#
# Two things have to stay true together, and nothing else in the suite looks
# at either: the route has to exist and stay cheap, and the vhost has to keep
# it out of the access log. The second matters because the log is the only
# record of who asks this site for what, and monitoring at a three-second
# cadence is 28,800 lines a day that nobody reads the log to find.
class HealthEndpointTest < ActionDispatch::IntegrationTest
  test 'up answers without touching anything' do
    get '/up'

    assert_response :success
    assert_equal 'ok', response.body
    assert_equal 'text/plain', response.media_type
  end

  # Not under `scope '(:locale)'`, deliberately. A health check that redirects
  # or negotiates a language is a health check that can fail for reasons that
  # have nothing to do with health.
  test 'up is not localized' do
    get '/ru/up'

    assert_not_equal 'ok', response.body,
                     'the healthcheck gained a locale-prefixed form, which deploy.sh does not use'
  end

  test 'the vhosts keep it out of the access log' do
    %w[org.openipc org.openipc.dev].each do |vhost|
      conf = Rails.root.join("deploy/nginx/sites-available/#{vhost}").read
      block = conf[%r{location = /up \{.*?\n    \}}m]

      assert block, "#{vhost} has no exact location for /up, so monitoring lands in the log"
      assert_match(/access_log off;/, block, <<~MESSAGE.chomp)
        #{vhost} logs /up. At a three-second monitoring cadence that is 28,800
        lines a day in the one record of who asks this site for what.
      MESSAGE
    end
  end
end
