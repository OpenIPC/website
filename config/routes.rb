Rails.application.routes.draw do
  # Healthcheck for the container runtime and the deploy script. Must stay above
  # the "*unmatched" catch-all below, which redirects instead of 404ing.
  get "/up", to: proc { [200, { "Content-Type" => "text/plain" }, ["ok"]] }

  root 'pages#home'

  get '/get-started', to: 'pages#get_started'
  get '/low-latency', to: 'pages#low_latency'
  get '/ecosystem',   to: 'pages#ecosystem'
  get '/business',    to: 'pages#business'
  get '/community',   to: 'pages#community'
  get '/donate',      to: 'pages#donate'

  # The pre-relaunch structure, redirected rather than dropped. These URLs are
  # in search results, in forum posts and in the wiki, and none of that is ours
  # to edit. 301 so the ones that are indexed transfer rather than compete.
  #
  # /home was the temporary URL the homepage answered on while it was being
  # built and nothing linked to it; it is the root now.
  get '/home',                to: redirect('/')
  get '/introduction',        to: redirect('/')
  get '/aaa',                 to: redirect('/')
  get '/fpv',                 to: redirect('/low-latency')
  get '/our-projects',        to: redirect('/ecosystem')
  get '/our-software',        to: redirect('/ecosystem')
  get '/our-channels',        to: redirect('/community')
  get '/support-open-source', to: redirect('/donate')
  # 302, not 301: /about is meant to become a page of its own, and a 301 is
  # cached by browsers indefinitely -- it would outlive the decision.
  get '/about',               to: redirect('/community', status: 302)
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
  get '/sponsor', to: redirect('/donate')

  get '/green_life', to:'pages#green_life'
  get '/merchandise', to: 'pages#merchandise'
  get '/our-team', to: 'pages#our_team'
  get '/stages-of-firmware-development', to: 'pages#stages_of_firmware_development'
  get '/utilities', to: 'pages#utilities'
  get '/web-interface', to: 'pages#web_interface'

  get '/supported-hardware', to: redirect('/supported-hardware/featured')
  get '/supported-hardware/featured', to: 'cameras/socs#featured'
  get '/supported-hardware/full-list', to: 'cameras/socs#full_list'

  # /tools/bandwidth-calculator is deliberately absent. It routed to
  # pages#bandwidth_calculator, which has never existed -- no action, no
  # template -- so every request raised AbstractController::ActionNotFound and
  # answered 500 in production. Falling through to the catch-all sends the
  # visitor to the homepage, which is at least a page.
  get '/tools/firmware-partitions-calculation', to: 'pages#firmware_partitions_calculation'
  get '/tools/high-resolution-timer', to: 'pages#high_resolution_timer'
  get '/tools/qr-code-generator', to: 'pages#qr_code_generator'
  # /tools/timelaps-interval-calculator is deliberately absent, for the same
  # reason as the bandwidth calculator above: pages#timelaps_interval_calculator
  # has no action and no template under that name, so the route has answered 500
  # for its whole life. app/views/pages/timelaps-interval-calculator.html.erb is
  # left in the tree -- it is an unfinished draft with hardcoded English and no
  # calculation, and finishing it is a decision for whoever started it, not
  # something to be done by a routing change.

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
