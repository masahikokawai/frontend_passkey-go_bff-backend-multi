-- | テスト専用の自プロセス内蔵JWKSサーバー(backend-java/backend-kotlin/backend-python/
-- backend-elixir/backend-c/backend-cppのモックJWKSサーバーと同じ役割)。実際にKeycloak/bffを
-- 起動せずにJwksVerifier/JwksCacheのRS256検証・kidキャッシュ・未知kid時の再取得ロジックを
-- 検証できる。`warp`(本番のREST実装と同じライブラリ)をそのままテスト用の使い捨てモックとしても
-- 再利用する
module MockJwksServer
  ( MockJwks
  , newMockJwks
  , addKey
  , resetKeys
  , mockJwksApp
  , withMockJwksServer
  ) where

import Data.Aeson (Value, object, (.=))
import Data.IORef (IORef, atomicModifyIORef', newIORef, readIORef)
import Data.Text (Text)
import Network.HTTP.Types (status200)
import Network.Wai (Application, responseLBS)
import Network.Wai.Handler.Warp (Port, testWithApplication)
import qualified Data.Aeson as Aeson

newtype MockJwks = MockJwks (IORef [Value])

newMockJwks :: IO MockJwks
newMockJwks = MockJwks <$> newIORef []

addKey :: MockJwks -> Text -> (Text, Text) -> IO ()
addKey (MockJwks ref) kid (n, e) =
  atomicModifyIORef' ref (\keys -> (keys ++ [keyJson kid n e], ()))

keyJson :: Text -> Text -> Text -> Value
keyJson kid n e = object ["kty" .= ("RSA" :: Text), "kid" .= kid, "use" .= ("sig" :: Text), "n" .= n, "e" .= e]

resetKeys :: MockJwks -> IO ()
resetKeys (MockJwks ref) = atomicModifyIORef' ref (const ([], ()))

mockJwksApp :: MockJwks -> Application
mockJwksApp (MockJwks ref) _req respond = do
  keys <- readIORef ref
  respond (responseLBS status200 [("Content-Type", "application/json")] (Aeson.encode (object ["keys" .= keys])))

-- | テストからは`around withMockJwksServer $ ... $ \(mock, port) -> ...`のように使う
-- (hspecの`around`は`ActionWith a -> IO ()`、すなわち1引数の継続を要求するため、タプルで渡す)。
-- portをJWKS URLの組み立てに使う
withMockJwksServer :: ((MockJwks, Port) -> IO a) -> IO a
withMockJwksServer action = do
  mock <- newMockJwks
  testWithApplication (pure (mockJwksApp mock)) (\port -> action (mock, port))
