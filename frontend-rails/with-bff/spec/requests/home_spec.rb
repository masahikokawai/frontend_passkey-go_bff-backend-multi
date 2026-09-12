require "rails_helper"

RSpec.describe "HomeController", type: :request do
  it "ルートページにログインリンクとTask一覧の入れ物が表示される(実データはブラウザ側JSがbff-rails経由で取得する)" do
    get "/"

    expect(response).to have_http_status(:ok)
    expect(response.body).to include("bff-rails")
    expect(response.body).to include('id="login-link"')
    expect(response.body).to include('id="tasks"')
    expect(response.body).to include("/api/auth/login")
  end

  # 【テスト監査で追加】
  # base64url変換ロジックをapp/javascript/webauthn_codec.jsへ切り出し、importmap経由でブラウザへ配信するよう変更した
  # (元はview内に直書きでテストが無く、実際にパディング計算のバグが見つかった)
  #
  # ここではrequest specの範囲でできる確認
  # (importmapタグ自体が描画され、webauthn_codecへのimport文がview内に残っていること)を
  # 押さえ、変換ロジックそのものの正しさはtest/javascript/webauthn_codec.test.js
  # (node --test)で担保する
  it "webauthn_codecモジュールをimportmap経由でロードする(base64url変換ロジックの二重管理を避けるため)" do
    get "/"

    expect(response.body).to include('type="importmap"')
    expect(response.body).to include("webauthn_codec")
  end

  # 【2回目のテスト監査で追加】
  # ログイン/登録の儀式ロジック(fetch呼び出し・credentialのJSON変換)も
  # app/javascript/passkey_client.jsへ切り出した
  # (元はwebauthn_codec.js切り出し後もview内にDOM操作と混ざったまま残っており、テストが1件も無かった)
  # 儀式ロジックそのものの正しさは test/javascript/passkey_client.test.js(node --test)で担保し、
  # ここでは view 側が実際にそのモジュールを import していることだけを確認する
  it "passkey_clientモジュールをimportmap経由でロードする(儀式ロジックの二重管理を避けるため)" do
    get "/"

    expect(response.body).to include("passkey_client")
    expect(response.body).to include(
      'import { loginWithPasskey, registerPasskeyCeremony } from "passkey_client"'
    )
  end
end
