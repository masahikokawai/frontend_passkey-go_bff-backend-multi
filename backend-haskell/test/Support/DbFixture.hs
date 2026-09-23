-- | 実DB(docker-compose上のMySQL)結合テスト共通のfixture。テストごとに一意なemail/keycloak_sub/
-- label名で行を作り、共有の開発用DBを汚さない(backend-java/backend-kotlin/backend-python/
-- backend-elixir/backend-c/backend-cppのDbFixture/DbTestFixtureと同じ設計。過去にbackend-rustで
-- 固定fixture行の共有が原因のテスト間干渉が見つかった経緯があるため、必ずテストごとに一意な行を作る)
module DbFixture
  ( pool
  , uniqueSuffix
  , createUser
  , createUserWithKeycloakSub
  , cleanupUser
  , createLabel
  , cleanupLabel
  , countTaskLabels
  ) where

import Data.Pool (Pool, defaultPoolConfig, newPool, setNumStripes, withResource)
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time (LocalTime, getCurrentTime, utc, utcToLocalTime)
import Data.Time.Clock.POSIX (getPOSIXTime)
import Data.Unique (hashUnique, newUnique)
import Database.MySQL.Base
import System.IO.Unsafe (unsafePerformIO)
import qualified System.IO.Streams as Streams

-- | プロセス内で1つだけ生成する共有プール。テストごとに毎回プールを張り直すのは無駄なため、
-- (テストコードに限り)`unsafePerformIO`+`NOINLINE`による遅延シングルトンパターンを使う
-- (本体側のMain.hsは通常のIOアクションとしてプールを構築しており、これは結合テストの
-- 便宜上の例外)
{-# NOINLINE pool #-}
pool :: Pool MySQLConn
pool = unsafePerformIO $ do
  let connInfo =
        defaultConnectInfoMB4
          { ciHost = "127.0.0.1"
          , ciPort = 13306
          , ciDatabase = "bff_gin_development"
          , ciUser = "root"
          , ciPassword = ""
          }
  newPool (setNumStripes (Just 1) (defaultPoolConfig (connect connInfo) close 30 5))

-- | 他言語のunique_suffix(マイクロ秒タイムスタンプ+乱数)相当。`Data.Unique`は
-- プロセス内で単調に増える一意な値を返すため、並行に走るテスト同士でも衝突しない
uniqueSuffix :: IO Text
uniqueSuffix = do
  posixMicros <- round . (* 1000000) <$> getPOSIXTime :: IO Integer
  u <- hashUnique <$> newUnique
  pure (T.pack (show posixMicros <> "-" <> show u))

nowLocal :: IO LocalTime
nowLocal = utcToLocalTime utc <$> getCurrentTime

createUser :: Text -> IO Int
createUser suffix = withResource pool $ \conn -> do
  now <- nowLocal
  _ <-
    execute
      conn
      "INSERT INTO users (email, name, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?)"
      [ MySQLText ("backend-haskell-test-" <> suffix <> "@example.com")
      , MySQLText ("backend-haskell-test-" <> suffix)
      , MySQLInt8U 1
      , MySQLDateTime now
      , MySQLDateTime now
      ]
  lastInsertIdOf conn

createUserWithKeycloakSub :: Text -> Text -> IO Int
createUserWithKeycloakSub suffix keycloakSub = do
  userId <- createUser suffix
  withResource pool $ \conn -> do
    now <- nowLocal
    _ <-
      execute
        conn
        "INSERT INTO user_keycloaks (user_id, keycloak_sub, created_at, updated_at) VALUES (?, ?, ?, ?)"
        [MySQLInt64 (fromIntegral userId), MySQLText keycloakSub, MySQLDateTime now, MySQLDateTime now]
    pure ()
  pure userId

-- | ベストエフォートの後始末(backend-java/backend-kotlin/backend-python/backend-elixirと同じ方針)
cleanupUser :: Int -> IO ()
cleanupUser userId = withResource pool $ \conn -> do
  _ <-
    execute
      conn
      "DELETE FROM task_labels WHERE task_id IN (SELECT id FROM tasks WHERE user_id = ?)"
      [MySQLInt64 (fromIntegral userId)]
  _ <- execute conn "DELETE FROM tasks WHERE user_id = ?" [MySQLInt64 (fromIntegral userId)]
  _ <- execute conn "DELETE FROM user_keycloaks WHERE user_id = ?" [MySQLInt64 (fromIntegral userId)]
  _ <- execute conn "DELETE FROM users WHERE id = ?" [MySQLInt64 (fromIntegral userId)]
  pure ()

createLabel :: Text -> IO Int
createLabel suffix = withResource pool $ \conn -> do
  now <- nowLocal
  _ <-
    execute
      conn
      "INSERT INTO labels (name, created_at, updated_at) VALUES (?, ?, ?)"
      [MySQLText ("backend-haskell-test-label-" <> suffix), MySQLDateTime now, MySQLDateTime now]
  lastInsertIdOf conn

cleanupLabel :: Int -> IO ()
cleanupLabel labelId = withResource pool $ \conn -> do
  _ <- execute conn "DELETE FROM labels WHERE id = ?" [MySQLInt64 (fromIntegral labelId)]
  pure ()

countTaskLabels :: Int -> IO Int
countTaskLabels taskId = withResource pool $ \conn -> do
  (_, is) <- query conn "SELECT COUNT(*) FROM task_labels WHERE task_id = ?" [MySQLInt64 (fromIntegral taskId)]
  rows <- Streams.toList is
  pure $ case rows of
    [[MySQLInt64 c]] -> fromIntegral c
    [[MySQLInt64U c]] -> fromIntegral c
    _ -> 0

lastInsertIdOf :: MySQLConn -> IO Int
lastInsertIdOf conn = do
  (_, is) <- query conn "SELECT LAST_INSERT_ID()" ([] :: [MySQLValue])
  rows <- Streams.toList is
  case rows of
    [[MySQLInt64 v]] -> pure (fromIntegral v)
    [[MySQLInt64U v]] -> pure (fromIntegral v)
    _ -> error "DbFixture: failed to read LAST_INSERT_ID()"
