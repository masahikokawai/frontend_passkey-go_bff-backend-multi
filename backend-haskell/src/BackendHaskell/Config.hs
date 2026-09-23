-- | backend-elixir/backend-python/backend-kotlin/backend-javaのenv var名・既定値と揃えている
-- (同じdocker-compose環境の上で比較実行できるようにするため)
module BackendHaskell.Config
  ( Config (..)
  , fromEnv
  ) where

import System.Environment (lookupEnv)
import Text.Read (readMaybe)

data Config = Config
  { httpAddr :: String
  , grpcAddr :: String
  , externalHttpAddr :: String
  , dbHost :: String
  , dbPort :: Int
  , dbUser :: String
  , dbPassword :: String
  , dbSchema :: String
  , keycloakIssuer :: String
  , keycloakJwksUrl :: String
  , expectedAudience :: String
  , localHmacSecret :: String
  , localRsaJwksUrl :: String
  , externalApiClientId :: String
  , logLevel :: String
  }

envOr :: String -> String -> IO String
envOr key fallback = do
  v <- lookupEnv key
  pure $ case v of
    Just "" -> fallback
    Just s -> s
    Nothing -> fallback

fromEnv :: IO Config
fromEnv = do
  issuer <- envOr "KEYCLOAK_ISSUER" "http://localhost:8082/realms/training"
  jwksUrlEnv <- lookupEnv "KEYCLOAK_JWKS_URL"
  let jwksUrl = case jwksUrlEnv of
        Just s | not (null s) -> s
        _ -> issuer <> "/protocol/openid-connect/certs"
  httpAddr' <- envOr "HTTP_ADDR" "8119"
  grpcAddr' <- envOr "GRPC_ADDR" "9105"
  externalHttpAddr' <- envOr "EXTERNAL_HTTP_ADDR" "8120"
  externalApiClientId' <- envOr "EXTERNAL_API_CLIENT_ID" "external-api-client"
  dbHost' <- envOr "DB_HOST" "127.0.0.1"
  dbPortRaw <- envOr "DB_PORT" "13306"
  dbUser' <- envOr "DB_USER" "root"
  dbPassword' <- envOr "DB_PASSWORD" ""
  dbSchema' <- envOr "DB_SCHEMA" "bff_gin_development"
  expectedAudience' <- envOr "EXPECTED_AUDIENCE" "backend"
  localHmacSecret' <- envOr "LOCAL_AUTH_HMAC_SECRET" "local-dev-hmac-shared-secret-change-me"
  localRsaJwksUrl' <- envOr "LOCAL_AUTH_RSA_JWKS_URL" "http://localhost:8080/.well-known/jwks.json"
  logLevel' <- envOr "LOG_LEVEL" "info"
  let dbPort' = maybe 13306 id (readMaybe dbPortRaw)
  pure
    Config
      { httpAddr = httpAddr'
      , grpcAddr = grpcAddr'
      , externalHttpAddr = externalHttpAddr'
      , dbHost = dbHost'
      , dbPort = dbPort'
      , dbUser = dbUser'
      , dbPassword = dbPassword'
      , dbSchema = dbSchema'
      , keycloakIssuer = issuer
      , keycloakJwksUrl = jwksUrl
      , expectedAudience = expectedAudience'
      , localHmacSecret = localHmacSecret'
      , localRsaJwksUrl = localRsaJwksUrl'
      , externalApiClientId = externalApiClientId'
      , logLevel = logLevel'
      }
