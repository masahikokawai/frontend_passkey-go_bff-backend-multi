-- | feature_flagsテーブルを10秒間隔で直接ポーリングするキャッシュ。JwksCache(kidごとの
-- 公開鍵キャッシュ、BackendHaskell.Auth.JwksCache参照)に続く、このコードベース2つ目の
-- STMベースの共有可変状態である。設計は同じ: `TVar`への読み書きを`atomically`ブロックで
-- 囲むだけで、ロックAPIは一切登場しない。JwksCacheは「未知のkidが来たときだけ再取得」する
-- オンデマンド更新だったのに対し、こちらは`forkIO`したバックグラウンドスレッドが
-- 10秒間隔で無条件にポーリングする点が異なる(bff/backend各言語のFeature Flagポーリングと
-- 同じ固定間隔戦略)
module BackendHaskell.Flags.FeatureFlagCache
  ( FeatureFlagCache
  , newFeatureFlagCache
  , startPolling
  , variation
  , pollOnce
  ) where

import Control.Concurrent (forkIO, threadDelay)
import qualified Control.Concurrent.STM as STM
import Control.Exception (SomeException, try)
import Data.Pool (Pool, withResource)
import qualified Data.Map.Strict as Map
import Data.Text (Text)
import qualified Data.Text.Encoding as TE
import Database.MySQL.Base
import qualified System.IO.Streams as Streams

data FlagEntry = FlagEntry
  { feEnabled :: Bool
  , feDefaultVariation :: Text
  }

newtype FeatureFlagCache = FeatureFlagCache (STM.TVar (Map.Map Text FlagEntry))

newFeatureFlagCache :: IO FeatureFlagCache
newFeatureFlagCache = FeatureFlagCache <$> STM.newTVarIO Map.empty

-- | バックグラウンドスレッドで10秒間隔のポーリングを開始する(README.md「Feature Flag
-- ポーリング」節参照)。ポーリング自体の失敗(DB接続断等)はキャッシュを直前の値に
-- 保ったまま次回まで待つ(bff/他言語と同じ「取得できなければ現状維持」の設計)
startPolling :: Pool MySQLConn -> FeatureFlagCache -> IO ()
startPolling pool cache = do
  _ <- forkIO loop
  pure ()
  where
    loop = do
      pollOnce pool cache
      threadDelay (10 * 1000 * 1000)
      loop

pollOnce :: Pool MySQLConn -> FeatureFlagCache -> IO ()
pollOnce pool (FeatureFlagCache tvar) = do
  result <- try (withResource pool fetchAll) :: IO (Either SomeException [(Text, Bool, Text)])
  case result of
    Left _ -> pure ()
    Right rows -> STM.atomically (STM.writeTVar tvar (Map.fromList [(k, FlagEntry en dv) | (k, en, dv) <- rows]))

fetchAll :: MySQLConn -> IO [(Text, Bool, Text)]
fetchAll conn = do
  (_, is) <- query_ conn "SELECT flag_key, enabled, default_variation FROM feature_flags"
  rows <- Streams.toList is
  pure (map rowToEntry rows)
  where
    rowToEntry [keyV, enabledV, defaultV] = (asText keyV, asBool enabledV, asText defaultV)
    rowToEntry row = error ("FeatureFlagCache: unexpected row shape: " ++ show row)
    asText (MySQLText t) = t
    asText (MySQLBytes b) = TE.decodeUtf8 b
    asText v = error ("FeatureFlagCache: expected text MySQLValue, got: " ++ show v)
    asBool (MySQLInt8U 1) = True
    asBool (MySQLInt8 1) = True
    asBool _ = False

-- | enabled=trueかつ登録済みならdefault_variation、そうでなければ呼び出し側のdefault_valueを
-- 返す(bff/他言語のFeature Flag評価と同じフォールバック規則)
variation :: FeatureFlagCache -> Text -> Text -> IO Text
variation (FeatureFlagCache tvar) flagKey defaultValue = do
  m <- STM.readTVarIO tvar
  pure $ case Map.lookup flagKey m of
    Just entry | feEnabled entry -> feDefaultVariation entry
    _ -> defaultValue
