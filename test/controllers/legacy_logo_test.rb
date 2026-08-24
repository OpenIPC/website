# frozen_string_literal: true

require 'test_helper'

# /images/logo_openipc.png is embedded on pages this project does not control,
# so it has to keep working -- but from our own infrastructure, not from a
# maintainer's personal domain.
class LegacyLogoTest < ActionDispatch::IntegrationTest
  test 'the legacy logo url redirects to an asset we serve' do
    get '/images/logo_openipc.png'
    assert_response :redirect

    uri = URI.parse(response.location)
    assert_match %r{\A/assets/logo_openipc-[0-9a-f]+\.png\z}, uri.path,
                 "expected our own fingerprinted asset, got #{uri.path}"
  end

  test 'it stays on this host rather than a third party' do
    get '/images/logo_openipc.png'

    uri = URI.parse(response.location)
    assert_equal 'www.example.com', uri.host, 'the redirect left our own host'
    assert_no_match(/themactep|cdn\./, response.location)
  end

  test 'the asset it names is one we ship' do
    assert File.exist?(Rails.root.join('app/assets/images/logo_openipc.png')),
           'the redirect target must be a file in this repository'
  end

  test 'no code still points at that CDN' do
    # Comments are allowed to name it -- routes.rb records why the redirect
    # changed. What must not come back is a line that actually uses it.
    offenders = Dir.glob(Rails.root.join('{app,config,lib}/**/*.{rb,erb,yml}')).select do |f|
      File.readlines(f).any? { |l| l.include?('cdn.themactep.com') && !l.strip.start_with?('#') }
    end
    assert_empty offenders.map { |f| f.sub("#{Rails.root}/", '') }
  end
end
