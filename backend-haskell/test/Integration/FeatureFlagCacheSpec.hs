-- | FeatureFlagCache(STM、JwksCacheに続く2つ目のTVarベースの共有可変状態)の
-- variation/pollOnceフォールバック規則を、実DBに一意なテスト専用flag_key行を挿入して検証する。
-- 他機能が参照する既存のflag行(backend.external-tasks-pagination-v2等)には一切触れない
module FeatureFlagCacheSpec (spec) where

import Control.Exception (finally)
import Data.Pool (withResource)
import Data.Text (Text)
import Data.Time (getCurrentTime, utc, utcToLocalTime)
import Database.MySQL.Base
import Test.Hspec

import BackendHaskell.Flags.FeatureFlagCache (newFeatureFlagCache, pollOnce, variation)
import DbFixture (pool, uniqueSuffix)

insertFlag :: Text -> Bool -> Text -> IO ()
insertFlag key enabled defaultVariation = withResource pool $ \conn -> do
  now <- utcToLocalTime utc <$> getCurrentTime
  _ <-
    execute
      conn
      "INSERT INTO feature_flags (flag_key, description, enabled, default_variation, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)"
      [ MySQLText key
      , MySQLText "backend-haskell test flag (FeatureFlagCacheSpec)"
      , MySQLInt8U (if enabled then 1 else 0)
      , MySQLText defaultVariation
      , MySQLDateTime now
      , MySQLDateTime now
      ]
  pure ()

deleteFlag :: Text -> IO ()
deleteFlag key = withResource pool $ \conn -> do
  _ <- execute conn "DELETE FROM feature_flags WHERE flag_key = ?" [MySQLText key]
  pure ()

spec :: Spec
spec = describe "FeatureFlagCache" $ do
  it "returns default_variation when enabled and found" $ do
    suffix <- uniqueSuffix
    let key = "backend-haskell-test-flag-" <> suffix
    insertFlag key True "on"
    ( do
        cache <- newFeatureFlagCache
        pollOnce pool cache
        result <- variation cache key "off"
        result `shouldBe` "on"
      )
      `finally` deleteFlag key

  it "returns fallback when disabled" $ do
    suffix <- uniqueSuffix
    let key = "backend-haskell-test-flag-" <> suffix
    insertFlag key False "on"
    ( do
        cache <- newFeatureFlagCache
        pollOnce pool cache
        result <- variation cache key "off"
        result `shouldBe` "off"
      )
      `finally` deleteFlag key

  it "returns fallback when not found" $ do
    cache <- newFeatureFlagCache
    pollOnce pool cache
    result <- variation cache "backend-haskell-test-flag-never-inserted" "off"
    result `shouldBe` "off"
