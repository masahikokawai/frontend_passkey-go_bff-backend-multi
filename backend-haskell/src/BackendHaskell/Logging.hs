-- | REST v1・外部公開APIのリクエスト単位ログ(gRPCの`grpc method=... status=... duration_ms=...`
-- と同じ形式、Grpc.TaskServiceの`logged`参照)と、`LOG_LEVEL`(backend(Go)/bff/gateway/goと同じ
-- env var名、既定`info`)によるDEBUGレベルの追加ログを提供する。
--
-- 【設計判断】このモジュールだけ、Config型を経由した明示的な値渡しではなく、
-- グローバルな`IORef`(`unsafePerformIO`+`NOINLINE`)でログレベルを保持する。
-- ログレベルはmain起動時に一度だけ設定され、以後は変化しない「実質的に不変な設定」であり、
-- JwksCache・UserResolver等、認証パス上の複数モジュールから横断的に参照する必要があるため、
-- それら全ての関数シグネチャにログレベルを引き回すよりも、この一点構成の方が見通しが良いと判断した
-- (backend-elixirのGenServerのような「状態を1箇所に閉じ込める」設計と同じ発想を、
-- Haskellでは「起動時に一度だけ書き込まれるIORef」という形で実現している)
module BackendHaskell.Logging
  ( setLogLevel
  , logDebug
  , requestLoggingMiddleware
  ) where

import Data.ByteString.Char8 (unpack)
import Data.IORef (IORef, newIORef, readIORef, writeIORef)
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Time.Clock as Clock
import Network.HTTP.Types.Status (statusCode)
import Network.Wai (Middleware, rawPathInfo, requestMethod, responseStatus)
import System.IO.Unsafe (unsafePerformIO)

{-# NOINLINE debugEnabledRef #-}
debugEnabledRef :: IORef Bool
debugEnabledRef = unsafePerformIO (newIORef False)

-- | main起動時に一度だけ呼ぶこと。`LOG_LEVEL=debug`のときだけ`logDebug`が実際に出力する
setLogLevel :: String -> IO ()
setLogLevel level = writeIORef debugEnabledRef (level == "debug")

-- | `LOG_LEVEL=debug`のときのみ出力する(既定`info`では何もしない)。
-- 第1引数はどこからのログかを示す文脈(例: "auth"・"external"・"jwks_cache")
logDebug :: Text -> Text -> IO ()
logDebug ctx msg = do
  enabled <- readIORef debugEnabledRef
  if enabled
    then putStrLn (T.unpack ("[debug] " <> ctx <> ": " <> msg))
    else pure ()

-- | REST v1・外部公開API共通のリクエスト単位ログ(常に出力、INFO相当)。
-- 第1引数はログの接頭辞("rest"または"external")
requestLoggingMiddleware :: String -> Middleware
requestLoggingMiddleware prefix app req respond = do
  start <- Clock.getCurrentTime
  app req $ \res -> do
    end <- Clock.getCurrentTime
    let durationMs = round (realToFrac (Clock.diffUTCTime end start) * 1000 :: Double) :: Int
        method = unpack (requestMethod req)
        path = unpack (rawPathInfo req)
        status = statusCode (responseStatus res)
    putStrLn
      ( prefix <> " method=" <> method <> " path=" <> path <> " status=" <> show status
          <> " duration_ms="
          <> show durationMs
      )
    respond res
