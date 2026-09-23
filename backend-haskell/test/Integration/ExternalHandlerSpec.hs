-- | 実際にWarpで外部公開APIサーバー(実DB接続)をテスト用ポートで起動し、http-clientで
-- 実際にHTTPリクエストを送って検証する結合テスト(backend-c/backend-cpp/backend-java/
-- backend-kotlin/backend-python/backend-elixirのExternalHandlerIntegrationTestと同じ
-- シナリオ構成)。認証にはこのファイル専用のモックJWKSサーバー(Keycloak相当)も
-- 自プロセス内に立てる
module ExternalHandlerSpec (spec) where

import Control.Concurrent (forkIO, threadDelay)
import Control.Exception (finally)
import Data.Aeson (Value (..))
import qualified Data.Aeson as Aeson
import qualified Data.Aeson.Key as AKey
import Data.Aeson.Types (parseMaybe, (.:))
import qualified Data.Foldable as Foldable
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import qualified Crypto.PubKey.RSA as RSA
import Database.MySQL.Base (MySQLValue (..), execute)
import Network.HTTP.Client hiding (Proxy)
import Network.HTTP.Types (hAuthorization, statusCode)
import Network.Wai.Handler.Warp (run)
import Servant (Proxy (..), serve)
import System.IO.Unsafe (unsafePerformIO)
import Test.Hspec

import BackendHaskell.Auth.Dispatcher
import BackendHaskell.Auth.HmacVerifier (newHmacVerifier)
import BackendHaskell.Auth.JwksCache (httpFetchViaClient, newJwksCache)
import BackendHaskell.Auth.JwksVerifier (newJwksVerifier)
import BackendHaskell.External.Api (ExternalTaskAPI)
import BackendHaskell.External.Server (externalServer)
import BackendHaskell.Flags.FeatureFlagCache (FeatureFlagCache, newFeatureFlagCache, pollOnce, startPolling)
import BackendHaskell.Domain.Models (TaskInput (..))
import qualified BackendHaskell.Repository.TaskRepository as Repo
import Data.Time.Calendar (fromGregorian)
import Data.Pool (withResource)

import DbFixture (cleanupUser, createUser, pool, uniqueSuffix)
import MockJwksServer (addKey, mockJwksApp, newMockJwks)
import TestTokenHelper

keycloakIssuer, audience, hmacSecret, expectedClientId :: Text
keycloakIssuer = "http://localhost:8082/realms/training"
audience = "backend"
hmacSecret = "external-test-hmac-secret-that-is-at-least-32-bytes-long"
expectedClientId = "external-api-client"

testExternalPort :: Int
testExternalPort = 18120

testJwksPort :: Int
testJwksPort = 19120

testKid :: Text
testKid = "external-test-kid"

flagKey :: Text
flagKey = "backend.external-tasks-pagination-v2"

-- | テストプロセス全体で1つだけ、外部公開APIサーバー・モックJWKSサーバー・Feature Flag
-- ポーラーを起動する(GrpcServiceSpec.hsと同じ「NOINLINE + unsafePerformIOによる
-- 遅延シングルトン」パターン)。RSA秘密鍵とFeatureFlagCacheはテスト本体から
-- 参照する必要があるため、`()`ではなくこれらの値を返す
{-# NOINLINE testEnv #-}
testEnv :: (RSA.PrivateKey, FeatureFlagCache, Manager)
testEnv = unsafePerformIO $ do
  mock <- newMockJwks
  (pub, priv) <- generateRsaKeyPair
  let (n, e) = publicJwkFields pub
  addKey mock testKid (n, e)
  _ <- forkIO (run testJwksPort (mockJwksApp mock))
  threadDelay 200000

  cache <- newJwksCache (T.pack ("http://127.0.0.1:" ++ show testJwksPort ++ "/jwks")) httpFetchViaClient
  let dispatcher =
        registerVerifier keycloakIssuer (newJwksVerifier cache keycloakIssuer audience)
          $ registerVerifier localHmacIssuer (newHmacVerifier hmacSecret localHmacIssuer audience)
          $ newDispatcher

  flagCache <- newFeatureFlagCache
  startPolling pool flagCache

  _ <-
    forkIO
      ( run
          testExternalPort
          (serve (Proxy :: Proxy ExternalTaskAPI) (externalServer pool dispatcher expectedClientId flagCache))
      )
  threadDelay 300000

  manager <- newManager defaultManagerSettings
  pure (priv, flagCache, manager)

validToken :: IO Text
validToken = do
  let (priv, _, _) = testEnv
  makeRsaTokenWithAzp priv testKid keycloakIssuer audience "external-caller" 3600 expectedClientId

-- | user_idはURLに直接埋め込み、page/page_size/cursor/limitはqueryとして渡す
getExternal :: Maybe Text -> Int -> [(String, String)] -> IO (Int, Value)
getExternal maybeBearer userId extraParams = do
  let (_, _, manager) = testEnv
      qs = ("user_id", show userId) : extraParams
      qsStr = intercalateAmp [k ++ "=" ++ v | (k, v) <- qs]
  req0 <- parseRequest ("http://127.0.0.1:" ++ show testExternalPort ++ "/external/v1/tasks?" ++ qsStr)
  let req = case maybeBearer of
        Nothing -> req0
        Just bearer -> req0 {requestHeaders = [(hAuthorization, TE.encodeUtf8 ("Bearer " <> bearer))]}
  resp <- httpLbs req manager
  let status = statusCode (responseStatus resp)
      body = maybe Null id (Aeson.decode (responseBody resp) :: Maybe Value)
  pure (status, body)
  where
    intercalateAmp [] = ""
    intercalateAmp [x] = x
    intercalateAmp (x : xs) = x ++ "&" ++ intercalateAmp xs

setPaginationFlag :: Bool -> IO ()
setPaginationFlag enabled = do
  withResource pool $ \conn -> do
    _ <-
      execute
        conn
        "UPDATE feature_flags SET enabled = ?, default_variation = ? WHERE flag_key = ?"
        [MySQLInt8U (if enabled then 1 else 0), MySQLText (if enabled then "on" else "off"), MySQLText flagKey]
    pure ()
  let (_, flagCache, _) = testEnv
  pollOnce pool flagCache

-- | 元の状態(enabled=0, default_variation='off')は全言語で共有されているため、
-- テスト前後で必ず復元する(backend-c/backend-cpp/backend-java/backend-kotlin/
-- backend-python/backend-elixirと同じ方針)
withRestoredFlag :: IO a -> IO a
withRestoredFlag action = action `finally` setPaginationFlag False

spec :: Spec
spec = around withUser $ describe "ExternalHandler" $ do
  it "rejects missing authorization" $ \_userId -> do
    (status, _) <- getExternal Nothing 1 []
    status `shouldBe` 401

  it "rejects wrong azp" $ \_userId -> do
    let (priv, _, _) = testEnv
    token <- makeRsaTokenWithAzp priv testKid keycloakIssuer audience "external-caller" 3600 "some-other-client"
    (status, _) <- getExternal (Just token) 1 []
    status `shouldBe` 401

  it "rejects a correctly-signed local HMAC token even with the correct azp" $ \_userId -> do
    token <- makeHmacTokenWithAzp hmacSecret localHmacIssuer audience "1" 3600 expectedClientId
    (status, _) <- getExternal (Just token) 1 []
    status `shouldBe` 401

  it "rejects missing user_id" $ \_userId -> do
    token <- validToken
    let (_, _, manager) = testEnv
    req <- parseRequest ("http://127.0.0.1:" ++ show testExternalPort ++ "/external/v1/tasks")
    let reqWithAuth = req {requestHeaders = [(hAuthorization, TE.encodeUtf8 ("Bearer " <> token))]}
    resp <- httpLbs reqWithAuth manager
    statusCode (responseStatus resp) `shouldBe` 400

  it "offset pagination across pages" $ \userId -> do
    token <- validToken
    ids <- mapM (const (createTaskFor userId)) [1 .. 3 :: Int]
    ( do
        (status1, body1) <- getExternal (Just token) userId [("page", "1"), ("page_size", "2")]
        status1 `shouldBe` 200
        totalOf body1 `shouldBe` Just (3 :: Int)
        length (tasksOf body1) `shouldBe` 2

        (status2, body2) <- getExternal (Just token) userId [("page", "2"), ("page_size", "2")]
        status2 `shouldBe` 200
        length (tasksOf body2) `shouldBe` 1
      )
      `finally` mapM_ (\tid -> Repo.delete pool tid userId) ids

  it "cursor pagination chains to null" $ \userId ->
    withRestoredFlag $ do
      setPaginationFlag True
      token <- validToken
      ids <- mapM (const (createTaskFor userId)) [1 .. 2 :: Int]
      ( do
          (status1, body1) <- getExternal (Just token) userId [("limit", "1")]
          status1 `shouldBe` 200
          length (tasksOf body1) `shouldBe` 1
          let Just cursor1 = nextCursorOf body1
          cursor1 `shouldNotBe` Null

          let cursorStr = case cursor1 of
                String s -> T.unpack s
                _ -> error "expected string cursor"
          (status2, body2) <- getExternal (Just token) userId [("limit", "1"), ("cursor", cursorStr)]
          status2 `shouldBe` 200
          length (tasksOf body2) `shouldBe` 1
          nextCursorOf body2 `shouldSatisfy` maybe False (const True)

          let cursorStr2 = case nextCursorOf body2 of
                Just (String s) -> T.unpack s
                _ -> error "expected string cursor"
          (status3, body3) <- getExternal (Just token) userId [("limit", "1"), ("cursor", cursorStr2)]
          status3 `shouldBe` 200
          tasksOf body3 `shouldBe` []
          nextCursorOf body3 `shouldBe` Just Null
        )
        `finally` mapM_ (\tid -> Repo.delete pool tid userId) ids
  where
    totalOf :: Value -> Maybe Int
    totalOf body = valueAt body "total" >>= asInt
    tasksOf :: Value -> [Value]
    tasksOf body = case valueAt body "tasks" of
      Just (Array arr) -> Foldable.toList arr
      _ -> []
    nextCursorOf :: Value -> Maybe Value
    nextCursorOf body = valueAt body "next_cursor"
    valueAt :: Value -> Text -> Maybe Value
    valueAt (Object o) key = flip parseMaybe o $ \obj -> obj .: AKey.fromText key
    valueAt _ _ = Nothing
    asInt :: Value -> Maybe Int
    asInt (Number n) = Just (round n)
    asInt _ = Nothing

createTaskFor :: Int -> IO Int
createTaskFor userId =
  Repo.create
    pool
    userId
    TaskInput {tiName = "ext-task", tiDescription = Nothing, tiStatusRaw = "waiting", tiFinishedOn = fromGregorian 2099 1 1, tiLabelIds = []}
    1

withUser :: (Int -> IO a) -> IO a
withUser action = do
  suffix <- uniqueSuffix
  userId <- createUser suffix
  action userId `finally` cleanupUser userId
