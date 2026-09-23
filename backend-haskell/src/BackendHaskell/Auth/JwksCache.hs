-- | kid(鍵ID)ごとの公開鍵キャッシュ。STM(Software Transactional Memory)で実装する。
--
-- 【このHaskell実装で最も学習価値の高い設計判断、13言語構成の並行処理モデル比較の締めくくり】
-- backend-kotlinはこのキャッシュを`Mutex`(kotlinx.coroutines.sync)で保護した共有Mapとして
-- 実装し(backend-kotlin/src/main/kotlin/com/bffgin/backend/auth/JwksVerifier.kt参照)、
-- backend-javaは`ConcurrentHashMap`(java.util.concurrent)を使い(backend-java/src/main/java/
-- com/bffgin/backend/auth/JwksVerifier.java参照)、backend-elixirはGenServer(メッセージ
-- パッシング、ロックという概念自体が無い、backend-elixir/lib/backend_elixir/auth/
-- jwks_cache_server.ex参照)を使った。
--
-- このHaskell実装は第4の解決策としてSTMを使う。`TVar`への読み書きを`atomically`ブロックで
-- 囲むだけで、ロックAPIを一切明示的に呼ばずに複合的な読み書きがアトミックかつコンポーザブルに
-- なる。内部的には楽観的並行性制御(競合が検出されたトランザクションは自動的に再試行される)で
-- 実現されており、Mutexのような「ロックを取る/離す」という手続き的な操作も、GenServerのような
-- 「別プロセスにメッセージを送る」という間接性も無い。型システムが`STM a`(トランザクション内)と
-- `IO a`(トランザクション外)の計算を区別し、`atomically :: STM a -> IO a`だけがこの2つを
-- つなぐ、という設計そのものが安全性の担保になっている。
module BackendHaskell.Auth.JwksCache
  ( JwksCache
  , HttpFetch
  , newJwksCache
  , lookupKey
  , refresh
  , httpFetchViaClient
  ) where

import qualified Control.Concurrent.STM as STM
import Control.Exception (SomeException, try)
import qualified Crypto.JOSE.JWK as JWK
import Crypto.Number.Serialize (os2ip)
import qualified Data.Aeson as Aeson
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString as BS
import qualified Data.ByteString.Lazy as BL
import qualified Data.Foldable as F
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import qualified Data.Text as T
import qualified Crypto.PubKey.RSA as RSA
import Network.HTTP.Client (httpLbs, newManager, parseRequest, responseBody, responseStatus)
import Network.HTTP.Client.TLS (tlsManagerSettings)
import Network.HTTP.Types.Status (statusCode)

import BackendHaskell.Auth.Dispatcher (decodeB64Url)
import BackendHaskell.Logging (logDebug)

-- | JWKS取得を差し替え可能にする(テスト時にモックJWKSサーバーへ向けるだけでよく、
-- HTTPクライアント自体を差し替える必要は無い)
type HttpFetch = Text -> IO (Either Text BS.ByteString)

data JwksCache = JwksCache
  { jcKeys :: STM.TVar (Map.Map Text JWK.JWK)
  , jcUrl :: Text
  , jcFetch :: HttpFetch
  }

newJwksCache :: Text -> HttpFetch -> IO JwksCache
newJwksCache url fetchFn = do
  tvar <- STM.newTVarIO Map.empty
  pure (JwksCache {jcKeys = tvar, jcUrl = url, jcFetch = fetchFn})

httpFetchViaClient :: HttpFetch
httpFetchViaClient url = do
  result <- try doFetch :: IO (Either SomeException (Either Text BS.ByteString))
  pure (either (const (Left "http_error")) id result)
  where
    doFetch = do
      manager <- newManager tlsManagerSettings
      req <- parseRequest (T.unpack url)
      resp <- httpLbs req manager
      if statusCode (responseStatus resp) == 200
        then pure (Right (BL.toStrict (responseBody resp)))
        else pure (Left "http_error")

lookupKey :: JwksCache -> Text -> IO (Maybe JWK.JWK)
lookupKey cache kid = Map.lookup kid <$> STM.readTVarIO (jcKeys cache)

-- | JWKSエンドポイントへ再取得を行い、キャッシュを丸ごと入れ替える。
-- 「kid不一致時のみ再取得」戦略は他言語と同じ(backend(Go)のjwks.go・backend-c/backend-cpp/
-- backend-java/backend-kotlin/backend-python/backend-elixirと同じ)。呼び出し元
-- (JwksVerifier.lookupOrRefresh)が「未知のkid -> refresh -> 再lookup」の流れを担う
refresh :: JwksCache -> IO (Either Text ())
refresh cache = do
  logDebug "jwks_cache" ("refreshing from " <> jcUrl cache)
  fetched <- jcFetch cache (jcUrl cache)
  case fetched of
    Left err -> pure (Left err)
    Right body -> case parseKeys body of
      Left err -> pure (Left err)
      Right newKeys -> do
        STM.atomically (STM.writeTVar (jcKeys cache) newKeys)
        logDebug "jwks_cache" ("refreshed, " <> T.pack (show (Map.size newKeys)) <> " key(s) cached")
        pure (Right ())

parseKeys :: BS.ByteString -> Either Text (Map.Map Text JWK.JWK)
parseKeys body = do
  obj <- maybe (Left "malformed_jwks") Right (Aeson.decodeStrict body :: Maybe Aeson.Object)
  keysVal <- maybe (Left "malformed_jwks") Right (KM.lookup "keys" obj)
  keysArr <- case keysVal of
    Aeson.Array arr -> Right arr
    _ -> Left "malformed_jwks"
  -- 【バッド/グッドプラクティス、backend-java/backend-kotlin/backend-python/backend-elixirと
  -- 同じ設計】特定の鍵1件の構築に失敗しただけでJWKS取得全体を失敗させると、他の正常な鍵まで
  -- 使えなくなってしまう。ここでは該当エントリだけスキップし(Data.Maybe.mapMaybeで捨てる)、
  -- 他の鍵は正常に反映する
  let parsed = foldr buildEntry [] (F.toList keysArr)
  Right (Map.fromList parsed)
  where
    buildEntry v acc = case buildOne v of
      Just entry -> entry : acc
      Nothing -> acc

buildOne :: Aeson.Value -> Maybe (Text, JWK.JWK)
buildOne (Aeson.Object o) = do
  Aeson.String kty <- KM.lookup "kty" o
  if kty /= "RSA" then Nothing else Just ()
  Aeson.String kid <- KM.lookup "kid" o
  let useOk = case KM.lookup "use" o of
        Just (Aeson.String "sig") -> True
        Nothing -> True
        _ -> False
  if not useOk then Nothing else Just ()
  Aeson.String nB64 <- KM.lookup "n" o
  Aeson.String eB64 <- KM.lookup "e" o
  nBytes <- either (const Nothing) Just (decodeB64Url nB64)
  eBytes <- either (const Nothing) Just (decodeB64Url eB64)
  let n = os2ip nBytes
      e = os2ip eBytes
      pubKey = RSA.PublicKey {RSA.public_size = BS.length nBytes, RSA.public_n = n, RSA.public_e = e}
      jwk = JWK.fromRSAPublic pubKey
  Just (kid, jwk)
buildOne _ = Nothing
