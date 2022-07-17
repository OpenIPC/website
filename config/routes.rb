Rails.application.routes.draw do
  root "pages#introduction"

  get '/aaa', to: 'pages#aaa'

  get '/coupler', to: redirect('https://openipc.github.io/coupler/')
  get '/firmware', to: redirect('https://openipc.github.io/firmware/')
  get '/ipctool', to: redirect('https://openipc.github.io/ipctool/')
  get '/microbe-web', to: redirect('https://openipc.github.io/microbe-web/')
  get '/smolrtsp', to: redirect('https://openipc.github.io/smolrtsp')
  get '/telemetry', to: redirect('https://openipc.github.io/telemetry/')
  get '/yaml-cli', to: redirect('https://openipc.github.io/yaml-cli/')
  get '/wiki', to: redirect('https://openipc.github.io/wiki/')

  get '/SDK', to: redirect('/supported-hardware')
  get '/sponsor', to: redirect('/support-open-source')

  get '/about', to: 'pages#about'
  get '/introduction', to:'pages#introduction'
  get '/our-projects', to: 'pages#our_projects'
  get '/our-team', to: 'pages#our_team'
  get '/our-channels', to: 'pages#our_channels'
  get '/stages-of-firmware-development', to: 'pages#stages_of_firmware_development'
  get '/support-open-source', to: 'pages#support_open_source'
  get '/supported-hardware', to: 'cameras/socs#index'
  get '/tools/firmware-partitions-calculation', to: 'pages#firmware_partitions_calculation'

  namespace :cameras do
    resources :vendors do
      resources :socs
    end
  end

  devise_for :admin
  namespace :admin do
    resources :socs
  end
  as :admin do
    get "/admin", to: "admin/dashboard#show", as: "admin_root"
    get "/admin/sign_out", to: "devise/sessions#destroy"
  end
end
