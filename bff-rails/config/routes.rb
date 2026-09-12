Rails.application.routes.draw do
  get "/api/auth/login", to: "auth#login"
  get "/auth/openid_connect/callback", to: "auth#callback"
  post "/api/auth/logout", to: "auth#logout"
  get "/api/me", to: "me#show"
  get "/api/tasks", to: "tasks#index"

  # CONTRACT.mdセクション22: パスキー(WebAuthn)。「webauthn」という技術用語だと
  # 何を登録するエンドポイントか分かりにくいため、パスに"passkey"を含める(ユーザー指定の命名規則)
  post "/api/auth/passkey/register/begin", to: "passkeys#register_begin"
  post "/api/auth/passkey/register/finish", to: "passkeys#register_finish"
  post "/api/auth/passkey/login/begin", to: "passkeys#login_begin"
  post "/api/auth/passkey/login/finish", to: "passkeys#login_finish"
end
