Rails.application.routes.draw do
  root "pages#introduction"

  get '/coupler', to: redirect('https://openipc.github.io/coupler/')
  get '/firmware', to: redirect('https://openipc.github.io/firmware/')
  get '/ipctool', to: redirect('https://openipc.github.io/ipctool/')
  get '/smolrtsp', to: redirect('https://openipc.github.io/smolrtsp')
  get '/telemetry', to: redirect('https://openipc.github.io/telemetry/')
  get '/yaml-cli', to: redirect('https://openipc.github.io/yaml-cli/')

  get '/sponsor', to: redirect('/support-open-source')

  get '/about', to: 'pages#about'
  get '/introduction', to:'pages#introduction'
  get '/tools/firmware-partitions-calculation', to: 'pages#firmware-partitions-calculation'
  get '/stages-of-firmware-development', to: 'pages#stages-of-firmware-development'
  get '/support-open-source', to: 'pages#support-open-source'
  get '/supported-hardware', to: 'cameras/socs#index'

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
