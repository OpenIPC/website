Rails.application.routes.draw do
  root "pages#introduction"

  post '/map_toolchain' => 'github#create'

  get '/aaa', to: 'pages#aaa'
  get '/majestic-endpoints', to: 'pages#majestic_endpoints'

  get '/coupler',     to: redirect('https://openipc.github.io/coupler/')
  get '/firmware',    to: redirect('https://openipc.github.io/firmware/')
  get '/ipctool',     to: redirect('https://openipc.github.io/ipctool/')
  get '/microbe-web', to: redirect('https://openipc.github.io/microbe-web/')
  get '/smolrtsp',    to: redirect('https://openipc.github.io/smolrtsp/')
  get '/telemetry',   to: redirect('https://openipc.github.io/telemetry/')
  get '/yaml-cli',    to: redirect('https://openipc.github.io/yaml-cli/')
  get '/wiki',        to: redirect('https://openipc.github.io/wiki/')

  get '/hardware',    to: redirect('/supported-hardware/featured')
  get '/ru/installation.md', to: redirect('https://wiki.openipc.org/ru/installation.html')
  get '/images/logo_openipc.png', to: redirect('https://cdn.themactep.com/images/logo_openipc.png')
  get '/devices/hs303/', to: redirect('https://wiki.openipc.org/ru/hardware-hs303.html')
  get '/install_switcam_hs303', to: redirect('https://wiki.openipc.org/ru/hardware-hs303.html')

  # FIXME: combine with above
  get '/coupler(/*any)',     to: redirect('https://openipc.github.io/coupler/%{any}')
  get '/firmware(/*any)',    to: redirect('https://openipc.github.io/firmware/%{any}')
  get '/ipctool(/*any)',     to: redirect('https://openipc.github.io/ipctool/%{any}')
  get '/microbe-web(/*any)', to: redirect('https://openipc.github.io/microbe-web/%{any}')
  get '/smolrtsp(/*any)',    to: redirect('https://openipc.github.io/smolrtsp/%{any}')
  get '/telemetry(/*any)',   to: redirect('https://openipc.github.io/telemetry/%{any}')
  get '/yaml-cli(/*any)',    to: redirect('https://openipc.github.io/yaml-cli/%{any}')
  get '/wiki(/*any)',        to: redirect('https://openipc.github.io/wiki/%{any}')

  get '/SDK', to: redirect('/supported-hardware')
  get '/sponsor', to: redirect('/support-open-source')

  get '/about', to: 'pages#about'
  get '/introduction', to:'pages#introduction'
  get '/our-projects', to: 'pages#our_projects'
  get '/our-team', to: 'pages#our_team'
  get '/our-channels', to: 'pages#our_channels'
  get '/stages-of-firmware-development', to: 'pages#stages_of_firmware_development'
  get '/support-open-source', to: 'pages#support_open_source'

  get '/supported-hardware', to: redirect('/supported-hardware/featured')
  get '/supported-hardware/featured', to: 'cameras/socs#featured'
  get '/supported-hardware/full-list', to: 'cameras/socs#full_list'

  get '/tools/bandwidth-calculator', to: 'pages#bandwidth_calculator'
  get '/tools/firmware-partitions-calculation', to: 'pages#firmware_partitions_calculation'
  get '/tools/high-resolution-timer', to: 'pages#high_resolution_timer'
  get '/tools/timelaps-interval-calculator', to: 'pages#timelaps-interval-calculator'

  get '/open-wall(/:page)', to: 'snapshots#index'
  get '/open-wall/camera/:id', to: 'snapshots#camera', as: 'openwall_camera'

  resources :binaries

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
  end
  as :admin do
    get "/admin", to: "admin/dashboard#show", as: "admin_root"
    get "/admin/sign_out", to: "devise/sessions#destroy"
  end

  match "*unmatched", to: "application#route_not_found",
        constraints: lambda { |req| req.path.exclude? 'rails/active_storage' },
        via: :all
end
