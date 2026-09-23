-- | user_id解決(UserResolver)・外部公開APIのクライアント認可(azp検証)に必要な
-- 最小限のクレームのみ保持する。azpはPhase 1(内部REST/gRPC)では不要だったため
-- 意図的に省略していたが、Phase 2(外部公開API)のRequireExternalClientAuth相当の
-- 検証(claims.azp == external_api_client_id)に必要なため追加した
module BackendHaskell.Auth.Claims
  ( Claims (..)
  ) where

import Data.Text (Text)

data Claims = Claims
  { claimsSub :: Text
  , claimsIss :: Text
  , claimsAzp :: Text
  }
  deriving (Eq, Show)
