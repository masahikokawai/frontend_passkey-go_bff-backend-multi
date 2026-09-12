# config.api_only = true にすると、cookies/sessionミドルウェアは既定で外される
# (config/application.rbのコメント参照)。bff-railsはブラウザにセッションCookieを
# 発行する必要があるため、Cookies用ミドルウェアだけを明示的に戻す
# (Rails自身のCookieStoreセッションは使わない。あくまでSessionStore(Redis)への
# 鍵として、生のCookie値を1つ読み書きするだけ)
Rails.application.config.middleware.use ActionDispatch::Cookies
