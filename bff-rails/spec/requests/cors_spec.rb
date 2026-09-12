require "rails_helper"

# 【テスト監査で追記】config/initializers/cors.rb には実装当初からテストが1件も無かった。
# frontend-rails/with-bff はブラウザ側JSからCookie付きfetch(credentials: "include")で
# bff-railsの/api/*を直接叩く設計(CONTRACT.mdセクション21.4)のため、CORS設定の
# 誤りは「ブラウザのconsoleにしかエラーが出ず、curlでの動作確認では気づけない」という
# 発見しづらい壊れ方をする。プリフライト(OPTIONS)・実リクエストの両方で、期待した
# オリジンにだけAccess-Control-Allow-*ヘッダが付与されることを確認する
RSpec.describe "CORS", type: :request do
  it "frontend-rails/with-bffのオリジンからのGETに、Allow-OriginとAllow-Credentialsが付与される" do
    get "/api/me", headers: { "Origin" => AppConfig.frontend_origin }

    expect(response.headers["Access-Control-Allow-Origin"]).to eq(AppConfig.frontend_origin)
    expect(response.headers["Access-Control-Allow-Credentials"]).to eq("true")
  end

  it "許可していないオリジンからのGETには、Allow-Originヘッダが付与されない" do
    get "/api/me", headers: { "Origin" => "http://evil.example.com" }

    expect(response.headers["Access-Control-Allow-Origin"]).to be_nil
  end

  it "プリフライト(OPTIONS)に対してもAllow-Originを返す" do
    options "/api/me", headers: {
      "Origin" => AppConfig.frontend_origin,
      "Access-Control-Request-Method" => "GET"
    }

    expect(response.headers["Access-Control-Allow-Origin"]).to eq(AppConfig.frontend_origin)
  end
end
