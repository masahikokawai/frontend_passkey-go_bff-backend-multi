# bff(Go)側と同じ3回目のe2e監査を受けて追加(bff/internal/auth/security_headers.go参照)。
# bff-railsもbffと同じ「クライアント専用のBFF」役割を担うため、同じヘッダーを同じ理由で付与する。
#
# - Cache-Control: no-store, Pragma: no-cache
#   認証状態に依存するAPIレスポンスがブラウザのbfcacheに乗ることでログアウト後も
#   一瞬古い画面が見える余地を防ぐ(OWASP Session Management Cheat Sheet)
# - X-Content-Type-Options: nosniff / X-Frame-Options: DENY
#   bffと同じ理由(MIMEスニッフィング対策・クリックジャッキング対策)
module SecurityHeaders
  extend ActiveSupport::Concern

  included do
    after_action :set_security_headers
  end

  private

  def set_security_headers
    response.headers["Cache-Control"] = "no-store"
    response.headers["Pragma"] = "no-cache"
    response.headers["X-Content-Type-Options"] = "nosniff"
    response.headers["X-Frame-Options"] = "DENY"
  end
end
