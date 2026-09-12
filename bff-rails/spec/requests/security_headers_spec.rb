require "rails_helper"

# bff(Go)側の同名テスト(bff/internal/auth/security_headers_test.go)と対になるテスト。
# 3回目のe2e監査で「明示的なキャッシュ制御ヘッダーが無い」という指摘を受けて追加した
# SecurityHeaders concernが、実際に全レスポンスへ適用されることを固定する
RSpec.describe "SecurityHeaders", type: :request do
  it "認証系エンドポイントのレスポンスに、Cache-Control等のセキュリティヘッダーが付与される" do
    get "/api/me"

    expect(response.headers["Cache-Control"]).to eq("no-store")
    expect(response.headers["Pragma"]).to eq("no-cache")
    expect(response.headers["X-Content-Type-Options"]).to eq("nosniff")
    expect(response.headers["X-Frame-Options"]).to eq("DENY")
  end
end
