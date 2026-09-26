# frozen_string_literal: true

require 'test_helper'

module Api
  module V1
    # The availability endpoint the prerendered hardware pages refresh from
    # (#162).
    class HardwareControllerTest < ActionDispatch::IntegrationTest
      test 'it answers JSON without a locale and without a session' do
        get '/api/v1/hardware/availability.json'

        assert_response :success
        assert_equal 'application/json', response.media_type
        # An endpoint a cache can hold: nothing per-visitor in it.
        assert_nil response.headers['Set-Cookie']
      end

      test 'it names every SoC and a state the page knows' do
        get '/api/v1/hardware/availability.json'
        body = response.parsed_body

        assert_equal Soc.count, body['socs'].size
        assert_equal Soc.all.map(&:urlname).sort, body['socs'].keys.sort

        body['socs'].each_value do |state|
          assert_includes %w[wizard firmware_only none], state
        end
      end

      test 'it says when it was generated, because the page decides whether to trust it' do
        get '/api/v1/hardware/availability.json'

        generated = Time.iso8601(response.parsed_body['generated_at'])
        assert_in_delta Time.current, generated, 60
      end
    end
  end
end
