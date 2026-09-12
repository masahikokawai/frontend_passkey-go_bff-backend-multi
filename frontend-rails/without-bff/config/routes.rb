Rails.application.routes.draw do
  # Define your application routes per the DSL in https://guides.rubyonrails.org/routing.html

  # Reveal health status on /up that returns 200 if the app boots with no exceptions, otherwise 500.
  # Can be used by load balancers and uptime monitors to verify that the app is live.
  get "up" => "rails/health#show", as: :rails_health_check

  # Render dynamic PWA files from app/views/pwa/* (remember to link manifest in application.html.erb)
  # get "manifest" => "rails/pwa#manifest", as: :pwa_manifest
  # get "service-worker" => "rails/pwa#service_worker", as: :pwa_service_worker

  # Defines the root path route ("/")
  root "sessions#login"

  get "login", to: "sessions#login"
  get "welcome", to: "sessions#welcome"
  get "account", to: "sessions#account"
  delete "logout", to: "sessions#logout"
  get "logout", to: "sessions#logout" # ブラウザから直接叩いての手動確認をしやすくするため許可

  # CONTRACT.mdセクション22.9: パスキー(WebAuthn)。bff/bff-railsは"/api/auth/passkey/*"だが、
  # このアプリは元々"/api"という名前空間を持たない(login/welcome等が直下)ため、
  # 既存の命名規則に合わせ"/auth/passkey/*"("/api"無し)にする
  post "auth/passkey/register/begin", to: "passkeys#register_begin"
  post "auth/passkey/register/finish", to: "passkeys#register_finish"
  post "auth/passkey/login/begin", to: "passkeys#login_begin"
  post "auth/passkey/login/finish", to: "passkeys#login_finish"

  # OmniAuthのcallback/failureはmiddleware(Rack::OmniAuth::Builder)が/authを丸ごと処理するため
  # 本来はconfig/routes.rbへの登録は不要だが、SessionsController#omniauth_callback/failureへの
  # 名前付きルートとして明示しておくとテスト・可読性の両面で扱いやすい
  get "auth/:provider/callback", to: "sessions#omniauth_callback"
  post "auth/:provider/callback", to: "sessions#omniauth_callback"
  get "auth/failure", to: "sessions#omniauth_failure"

  # frontend-rails.oidc-gem=openid_connect(手組み型)のときだけ、OidcGemDispatcherが
  # このパスをOmniAuthへ渡さずRailsルーティングへ素通しする(=このアクションに実際に届く)
  # omniauth-openid-connect(お任せ型)選択時はOmniAuth::Builderが横取りするため、
  # このルートは定義上存在するが到達しない
  post "auth/openid_connect", to: "sessions#start_login"
end
