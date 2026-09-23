module Main (main) where

import Control.Concurrent (forkIO)
import Data.Pool (defaultPoolConfig, newPool, setNumStripes)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import Database.MySQL.Base (ConnectInfo (..), close, connect, defaultConnectInfoMB4)
import Network.GRPC.Common
import Network.GRPC.Server.Run
import Network.GRPC.Server.StreamType (fromMethods)
import Network.Wai.Handler.Warp (run)
import Servant
import System.IO (BufferMode (LineBuffering), hSetBuffering, stdout)

import BackendHaskell.Auth.Dispatcher
import BackendHaskell.Auth.HmacVerifier (newHmacVerifier)
import BackendHaskell.Auth.JwksCache (httpFetchViaClient, newJwksCache)
import BackendHaskell.Auth.JwksVerifier (newJwksVerifier)
import BackendHaskell.Config (Config (..), fromEnv)
import BackendHaskell.External.Api (ExternalTaskAPI)
import BackendHaskell.External.Server (externalServer)
import BackendHaskell.Flags.FeatureFlagCache (newFeatureFlagCache, startPolling)
import BackendHaskell.Grpc.TaskService (taskServiceMethods)
import BackendHaskell.Logging (requestLoggingMiddleware, setLogLevel)
import BackendHaskell.Rest.Api (TaskAPI)
import BackendHaskell.Rest.Server (taskServer)

main :: IO ()
main = do
  -- 【実機検証で見つかった実バグ】stdoutがTTYではなくファイル/パイプへリダイレクトされると、
  -- GHCランタイムの既定はブロックバッファリングになり、プロセスが終了するまでログが
  -- 一切flushされない(putStrLnを呼んでいるのに、tail等で全く見えない)。
  -- 本番相当の運用(stdoutをログ収集基盤へリダイレクトする)を想定し、行バッファリングに
  -- 明示的に切り替える(1行ごとに確実にflushされる)
  hSetBuffering stdout LineBuffering
  config <- fromEnv
  setLogLevel (logLevel config)
  putStrLn
    ( "backend-haskell starting: HTTP_ADDR=:" <> httpAddr config <> " GRPC_ADDR=:" <> grpcAddr config
        <> " EXTERNAL_HTTP_ADDR=:"
        <> externalHttpAddr config
        <> " db="
        <> dbHost config
        <> ":"
        <> show (dbPort config)
        <> "/"
        <> dbSchema config
    )

  let connInfo =
        defaultConnectInfoMB4
          { ciHost = dbHost config
          , ciPort = fromIntegral (dbPort config)
          , ciDatabase = TE.encodeUtf8 (T.pack (dbSchema config))
          , ciUser = TE.encodeUtf8 (T.pack (dbUser config))
          , ciPassword = TE.encodeUtf8 (T.pack (dbPassword config))
          }
  pool <- newPool (setNumStripes (Just 1) (defaultPoolConfig (connect connInfo) close 30 10))

  localRsaCache <- newJwksCache (T.pack (localRsaJwksUrl config)) httpFetchViaClient
  keycloakCache <- newJwksCache (T.pack (keycloakJwksUrl config)) httpFetchViaClient

  let dispatcher =
        registerVerifier
          (T.pack (keycloakIssuer config))
          (newJwksVerifier keycloakCache (T.pack (keycloakIssuer config)) (T.pack (expectedAudience config)))
          $ registerVerifier
            localRsaIssuer
            (newJwksVerifier localRsaCache localRsaIssuer (T.pack (expectedAudience config)))
          $ registerVerifier
            localHmacIssuer
            (newHmacVerifier (T.pack (localHmacSecret config)) localHmacIssuer (T.pack (expectedAudience config)))
          $ newDispatcher

  _ <-
    forkIO
      ( run
          (readPort (httpAddr config))
          (requestLoggingMiddleware "rest" (serve (Proxy :: Proxy TaskAPI) (taskServer pool dispatcher)))
      )

  -- 外部公開API(:8120予定、CONTRACT.mdセクション11)。内部REST v1とは別の、
  -- 独立したWarpリスナー(認証モデルが異なるため、後述のREADME.md参照)
  flagCache <- newFeatureFlagCache
  startPolling pool flagCache
  _ <-
    forkIO
      ( run
          (readPort (externalHttpAddr config))
          ( requestLoggingMiddleware
              "external"
              (serve (Proxy :: Proxy ExternalTaskAPI) (externalServer pool dispatcher (T.pack (externalApiClientId config)) flagCache))
          )
      )

  let grpcConfig =
        ServerConfig
          { serverInsecure = Just (InsecureConfig (Just "0.0.0.0") (fromIntegral (readPort (grpcAddr config))))
          , serverSecure = Nothing
          }
  putStrLn ("backend-haskell listening: REST=:" <> httpAddr config <> " EXTERNAL=:" <> externalHttpAddr config <> " GRPC=:" <> grpcAddr config)
  runServerWithHandlers def grpcConfig (fromMethods (taskServiceMethods pool dispatcher))

readPort :: String -> Int
readPort = read
