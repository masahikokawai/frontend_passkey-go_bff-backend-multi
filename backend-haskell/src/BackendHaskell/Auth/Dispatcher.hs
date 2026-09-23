-- | JWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
-- (backend(Go)のauthjwt.Dispatcher・backend-rust/backend-c/backend-cpp/backend-java/
-- backend-kotlin/backend-python/backend-elixirと同じ2段構造)。issの詐称は委譲先の署名検証で
-- 弾かれる:「振り分けのために覗く」ことと「検証を信頼する」ことは別、という2段階構造を維持する。
--
-- Haskellにはinterfaceが無いため(型クラスは開いた多相だがdispatcher内に異種の検証器を
-- 値として詰め込むには向かない)、検証器は単に「トークン文字列を受け取りClaimsを返すIOアクション」
-- という関数型として登録する(Elixirの`{module, arg}`タプルによるダックタイピング的設計と
-- 同じ発想を、Haskellでは第一級関数としてそのまま表現できる)
module BackendHaskell.Auth.Dispatcher
  ( Dispatcher
  , VerifierFn
  , localHmacIssuer
  , localRsaIssuer
  , isLocalIssuer
  , newDispatcher
  , registerVerifier
  , dispatchVerify
  , splitToken
  , peekIssuer
  , decodeB64Url
  ) where

import qualified Data.Aeson as Aeson
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString as BS
import qualified Data.ByteString.Base64.URL as B64URL
import qualified Data.ByteString.Char8 as BC
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE

import BackendHaskell.Auth.Claims (Claims)

type VerifierFn = Text -> IO (Either Text Claims)

newtype Dispatcher = Dispatcher (Map.Map Text VerifierFn)

localHmacIssuer :: Text
localHmacIssuer = "bff-gin-local-hmac"

localRsaIssuer :: Text
localRsaIssuer = "bff-gin-local-rsa"

isLocalIssuer :: Text -> Bool
isLocalIssuer iss = iss == localHmacIssuer || iss == localRsaIssuer

newDispatcher :: Dispatcher
newDispatcher = Dispatcher Map.empty

registerVerifier :: Text -> VerifierFn -> Dispatcher -> Dispatcher
registerVerifier issuer verifier (Dispatcher m) = Dispatcher (Map.insert issuer verifier m)

-- | "header.payload.signature" を3分割する
splitToken :: Text -> Either Text (Text, Text, Text)
splitToken token = case T.splitOn "." token of
  [h, p, s] -> Right (h, p, s)
  _ -> Left "malformed_token"

-- | base64url(パディング無し)decode。JWTの各セグメントはパディング無しでエンコードされる
-- 慣例のため、decodeの前に'='でパディングを補う
decodeB64Url :: Text -> Either Text BS.ByteString
decodeB64Url t =
  case B64URL.decode (padded (TE.encodeUtf8 t)) of
    Right bs -> Right bs
    Left _ -> Left "malformed_token"
  where
    padded bs = bs <> BC.replicate ((4 - BS.length bs `mod` 4) `mod` 4) '='

-- | payload部をbase64url decode + JSON parseし、"iss"クレームだけを覗く
-- (署名検証前、詐称されている可能性を織り込んだ「覗き見」であることに注意)
peekIssuer :: Text -> Either Text Text
peekIssuer payloadB64 = do
  raw <- decodeB64Url payloadB64
  obj <- maybe (Left "malformed_token") Right (Aeson.decodeStrict raw :: Maybe Aeson.Object)
  case KM.lookup "iss" obj of
    Just (Aeson.String iss) -> Right iss
    _ -> Left "unknown_issuer"

dispatchVerify :: Dispatcher -> Text -> IO (Either Text Claims)
dispatchVerify (Dispatcher m) token =
  case lookupVerifier of
    Left err -> pure (Left err)
    Right verifier -> verifier token
  where
    lookupVerifier = do
      (_, payloadB64, _) <- splitToken token
      iss <- peekIssuer payloadB64
      maybe (Left "unknown_issuer") Right (Map.lookup iss m)
