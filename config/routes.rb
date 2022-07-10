Rails.application.routes.draw do
  root "socs#index"
  get '/tools/firmware-partitions-calculation', to: 'pages#calc'
  get '/socs/legend', to: 'socs#legend'

  resources :socs

  devise_for :admin
  namespace :admin do
    resources :socs
  end
  as :admin do
    get "/admin", to: "admin/dashboard#show", as: "admin_root"
    get "/admin/sign_out", to: "devise/sessions#destroy"
    # match "/admin/stats/monthly(/:year(/:month))", to: "admin/stats#monthly", as: "admin_monthly_stats", via: [:get, :post]
    # match "/admin/stats/totals(/:year(/:month))", to: "admin/stats#totals", as: "admin_totals_stats", via: [:get, :post]
  end
end
