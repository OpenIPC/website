Rails.application.routes.draw do
  # Healthcheck for the container runtime and the deploy script. Must stay above
  # the "*unmatched" catch-all below, which redirects instead of 404ing.
  get "/up", to: proc { [200, { "Content-Type" => "text/plain" }, ["ok"]] }

  root "pages#introduction"

  get '/aaa', to: 'pages#aaa'

  # The relaunched pages. They answer on their own URLs from here, so they can be
  # reviewed and deployed on their own, but nothing links to them yet: the root
  # route and the navigation still serve the pre-relaunch structure. The cutover
  # is a separate change.
  get '/get-started', to: 'pages#get_started'
  get '/low-latency', to: 'pages#low_latency'
  get '/ecosystem',   to: 'pages#ecosystem'
  get '/business',    to: 'pages#business'
  get '/community',   to: 'pages#community'
  get '/donate',      to: 'pages#donate'
  get '/home',        to: 'pages#home'
  get '/majestic-endpoints', to: 'pages#majestic_endpoints'

  get '/coupler',     to: redirect('https://github.com/openipc//coupler/')
  get '/firmware',    to: redirect('https://github.com/openipc//firmware/')
  get '/ipctool',     to: redirect('https://github.com/openipc/ipctool/')
  get '/microbe-web', to: redirect('https://github.com/openipc/microbe-web/')
  get '/smolrtsp',    to: redirect('https://github.com/openipc/smolrtsp/')
  get '/telemetry',   to: redirect('https://github.com/openipc/telemetry/')
  get '/yaml-cli',    to: redirect('https://github.com/openipc/yaml-cli/')
  get '/wiki',        to: redirect('https://github.com/openipc/wiki/')

  get '/hardware',    to: redirect('/supported-hardware/featured')
  get '/ru/installation.md', to: redirect('https://github.com/OpenIPC/wiki/blob/master/ru/installation.md')
  # A legacy URL people still embed elsewhere -- five hits a fortnight, but
  # they are somebody else's pages and should not break. It used to redirect to
  # cdn.themactep.com, which is a maintainer's personal domain rather than
  # anything this project runs; the image is now ours and served from our own
  # assets. Resolved per request so it follows the fingerprint.
  get '/images/logo_openipc.png',
      to: redirect { |_params, _request| ActionController::Base.helpers.asset_path('logo_openipc.png') }
  get '/devices/hs303/', to: redirect('https://github.com/OpenIPC/wiki/blob/master/ru/hardware-hs303.md')
  get '/install_switcam_hs303', to: redirect('https://github.com/OpenIPC/wiki/blob/master/ru/hardware-hs303.md')

  # FIXME: combine with above
  get '/coupler(/*any)',     to: redirect('https://github.com/openipc//coupler')
  get '/firmware(/*any)',    to: redirect('https://github.com/openipc/firmware')
  get '/ipctool(/*any)',     to: redirect('https://github.com/openipc/ipctool')
  get '/microbe-web(/*any)', to: redirect('https://github.com/openipc/microbe-web')
  get '/smolrtsp(/*any)',    to: redirect('https://github.com/openipc/smolrtsp')
  get '/telemetry(/*any)',   to: redirect('https://github.com/openipc/telemetry')
  get '/yaml-cli(/*any)',    to: redirect('https://github.com/openipc/yaml-cli')
  get '/wiki(/*any)',        to: redirect('https://github.com/openipc/wiki')

  get '/SDK', to: redirect('/supported-hardware')
  get '/sponsor', to: redirect('/support-open-source')

  get '/about', to: 'pages#about'
  get '/green_life', to:'pages#green_life'
  get '/introduction', to:'pages#introduction'
  get '/merchandise', to: 'pages#merchandise'
  get '/our-projects', to: 'pages#our_projects'
  get '/our-software', to: 'pages#our_software'
  get '/our-team', to: 'pages#our_team'
  get '/our-channels', to: 'pages#our_channels'
  get '/stages-of-firmware-development', to: 'pages#stages_of_firmware_development'
  get '/utilities', to: 'pages#utilities'
  get '/support-open-source', to: 'pages#support_open_source'
  get '/web-interface', to: 'pages#web_interface'

  get '/supported-hardware', to: redirect('/supported-hardware/featured')
  get '/supported-hardware/featured', to: 'cameras/socs#featured'
  get '/supported-hardware/full-list', to: 'cameras/socs#full_list'

  get '/tools/bandwidth-calculator', to: 'pages#bandwidth_calculator'
  get '/tools/firmware-partitions-calculation', to: 'pages#firmware_partitions_calculation'
  get '/tools/high-resolution-timer', to: 'pages#high_resolution_timer'
  get '/tools/qr-code-generator', to: 'pages#qr_code_generator'
  get '/tools/timelaps-interval-calculator', to: 'pages#timelaps_interval_calculator'

  # Commented out in ed0e025, a bulk tidy-up, while five places that redirect to
  # /open-wall were left in: snapshots_controller.rb twice,
  # admin/snapshots_controller.rb, and the breadcrumb on three views. Every one
  # of them fell through to the catch-all and answered a 302 to the homepage.
  #
  # This exposes nothing new. `resources :snapshots` has served the same gallery
  # at /snapshots throughout; these are the URLs the site itself uses for it.
  get '/open-wall/camera/:id', to: 'snapshots#camera', as: 'openwall_camera'
  get '/open-wall(/:page)', to: 'snapshots#index', as: 'open_wall'

  resources :snapshots do
    get :camera, on: :collection
    get :oneday, on: :member
    get :download, on: :member
  end

  namespace :cameras do
    resources :socs
    resources :vendors do
      resources :socs do
        get :download_full_image, on: :member
      end
    end
  end

  devise_for :admin
  namespace :admin do
    resources :snapshots
    resources :socs
    resources :vendors
  end
  as :admin do
    get "/admin", to: "admin/dashboard#show", as: "admin_root"
    get "/admin/sign_out", to: "devise/sessions#destroy"
  end

  # Retired 2026-08. Falling through to the catch-all below would answer
  # 302-then-200 at the homepage, which for a page whose remaining traffic is
  # entirely scripted is a lie -- the fortnight before it went, 25 of its 26
  # fetches carried a curl or Wget agent. 410 tells them to stop asking.
  match "/binaries", to: proc { [410, { "Content-Type" => "text/plain" }, ["Gone\n"]] },
        via: :all, as: :retired_binaries

  match "*unmatched", to: "application#route_not_found",
        constraints: lambda { |req| req.path.exclude? 'rails/active_storage' },
        via: :all
end
