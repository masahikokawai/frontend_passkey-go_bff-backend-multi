Rails.application.routes.draw do
  root "feature_flags#index"

  resources :feature_flags, only: %i[index edit update] do
    member do
      get :audit_logs
    end
  end

  resources :users, only: %i[index create destroy] do
    member do
      patch :update_role
    end
  end
end
