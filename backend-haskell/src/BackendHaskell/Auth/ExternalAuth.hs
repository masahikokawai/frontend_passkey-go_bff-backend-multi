-- | 外部公開API(CONTRACT.mdセクション11)専用の認可検証。内部REST/gRPCのuser_id解決
-- (UserResolver)とは別の関数として分離する(backend-c/backend-cpp/backend-java/
-- backend-kotlin/backend-python/backend-elixirのRequireExternalClientAuth相当)。
--
-- 内部トランスポートとの決定的な違いは2点:
--   1. ローカル発行(HMAC/RSA)issuerのJWTは、正しく署名されていても拒否する
--      (Client Credentials Grant、つまりKeycloak発行のトークンのみを受け付ける)
--   2. claims.azp(トークンを取得したOAuth2クライアントのclient_id)が
--      external_api_client_idと一致することを要求する
--
-- 外部公開APIはuser_idをクライアントが指定するクエリパラメータとして信頼する設計
-- (CONTRACT.mdセクション11の既知の制約)であり、JWTのsubからuser_idを解決する
-- 内部トランスポートとは異なるため、成功時にClaimsやuser_idを返す必要は無い
module BackendHaskell.Auth.ExternalAuth
  ( requireExternalClient
  ) where

import Data.Text (Text)
import qualified Data.Text as T

import BackendHaskell.Auth.Claims (Claims (..))
import BackendHaskell.Auth.Dispatcher (Dispatcher, dispatchVerify, isLocalIssuer)
import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (Unauthorized))

requireExternalClient :: Dispatcher -> Text -> Maybe Text -> IO (Either TaskError ())
requireExternalClient dispatcher expectedClientId maybeAuthHeader =
  case maybeAuthHeader >>= stripBearer of
    Nothing -> pure (Left unauthorized)
    Just token -> do
      verifyResult <- dispatchVerify dispatcher token
      pure $ case verifyResult of
        Left _ -> Left unauthorized
        Right claims
          | isLocalIssuer (claimsIss claims) -> Left unauthorized
          | claimsAzp claims /= expectedClientId -> Left unauthorized
          | otherwise -> Right ()

-- | UserResolver.hsのstripBearerと同じ実装(重複だが2箇所とも十分に小さいため、
-- 共通モジュールへ切り出す優先度は低いと判断している)
stripBearer :: Text -> Maybe Text
stripBearer header =
  let prefix = "Bearer "
   in if T.isPrefixOf prefix header && T.length header > T.length prefix
        then Just (T.drop (T.length prefix) header)
        else Nothing

unauthorized :: TaskError
unauthorized = TaskError Unauthorized "unauthorized"
