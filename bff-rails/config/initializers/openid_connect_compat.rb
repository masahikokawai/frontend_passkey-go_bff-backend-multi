# 【実装時に発見した実際の非互換性】openid_connect 0.9.2(2015年頃のリリース)の
# Client#handle_success_responseは`JSON.parse(response.body)`を呼ぶ実装だが、
# 同時に依存しているrack-oauth2 2.3.0(現行バージョン)の`Rack::OAuth2.http_client`は
# デフォルトで`faraday.response :json`ミドルウェアを組み込んでおり、この時点で
# response.bodyは既にHashへパース済みになっている。結果、
# `JSON.parse(既にHashのbody)`が`TypeError: no implicit conversion of Hash into String`で
# 必ず落ちる(bundlerが解決した2gemの組み合わせが、実際には想定されていなかった非互換)。
# response.bodyがStringかHashかで分岐する、最小限の互換シムで対応する
module OpenIDConnectHandleSuccessResponseCompat
  def handle_success_response(response)
    body = response.body
    token_hash = (body.is_a?(String) ? JSON.parse(body) : body).with_indifferent_access
    case token_hash[:token_type].try(:downcase)
    when "bearer"
      ::OpenIDConnect::AccessToken.new(token_hash.merge(client: self))
    else
      raise ::OpenIDConnect::Exception.new("Unexpected Token Type: #{token_hash[:token_type]}")
    end
  rescue JSON::ParserError
    raise ::OpenIDConnect::Exception.new("Unknown Token Type")
  end
end

Rails.application.config.to_prepare do
  OpenIDConnect::Client.prepend(OpenIDConnectHandleSuccessResponseCompat)
end
