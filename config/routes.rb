Rails.application.routes.draw do
  root "socs#index"
  resources :socs

  get '/tools/firmware-partitions-calculation', to: 'pages#calc'

  namespace :admin do
    resources :socs
  end
end
