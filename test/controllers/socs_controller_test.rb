# frozen_string_literal: true

require 'test_helper'

class SocsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @vendor = Vendor.create!(name: 'Testco')
    # status drives image_tag("stage-#{status}.svg"), which sprockets raises on
    # if the file is missing, and the blank filenames keep instructable? false
    # so the row renders without building an installation link.
    @soc = Soc.create!(vendor: @vendor, model: 'TS3516EV300', status: 'done',
                       uboot_filename: '', linux_filename: '')
  end

  # show_exceptions is off in this environment, so the exception reaches the
  # test rather than being rendered. In production RescueHandler turns
  # RecordNotFound into public/404.html; what matters here is that it is
  # RecordNotFound at all, and not the NoMethodError on nil that Firmware
  # #generate used to raise -- which surfaced as a 500.
  test 'an unknown SoC slug is not found rather than a server error' do
    assert_raises(ActiveRecord::RecordNotFound) do
      get "/cameras/vendors/#{@vendor.to_param}/socs/no-such-soc/download_full_image",
          params: { flash_size: 8, flash_type: 'nor', fw_release: 'lite' }
    end
  end

  test 'an unknown vendor slug is not found either' do
    assert_raises(ActiveRecord::RecordNotFound) do
      get '/cameras/vendors/no-such-vendor'
    end
  end

  # The SoC index is a vendor filter and nothing else. The template calls
  # `render @socs` unconditionally, so the action leaving it unset answered 500
  # for anyone who reached /cameras/socs without one.
  test 'the SoC index sends a request with no vendor to the featured page' do
    get '/cameras/socs'

    assert_redirected_to '/supported-hardware/featured'
  end

  # No test renders a page here. The layout asks the pipeline for
  # application.css, app/assets/builds/ is gitignored, and the workflow runs
  # `bin/rails test` without building assets first -- so a full render errors
  # with "The asset \"application.css\" is not present in the asset pipeline"
  # for reasons that have nothing to do with the code under test. The
  # rendering path is checked against the running site instead.

  test 'the SoC index does not find a vendor that does not exist' do
    assert_raises(ActiveRecord::RecordNotFound) do
      get '/cameras/socs', params: { vendor: 'no-such-vendor' }
    end
  end
end
