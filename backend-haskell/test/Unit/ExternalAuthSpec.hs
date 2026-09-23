-- | 外部公開API専用の認可検証(RequireExternalClientAuth相当)のテスト。
-- 「正しく署名されたローカルHMACトークンでも、issuerがローカルであるという理由だけで
-- 拒否される」という、内部トランスポートとの決定的な違いが本テストの核心
module ExternalAuthSpec (spec) where

import qualified Data.Text as T
import Test.Hspec

import BackendHaskell.Auth.Dispatcher
import BackendHaskell.Auth.ExternalAuth (requireExternalClient)
import BackendHaskell.Auth.HmacVerifier (newHmacVerifier)
import BackendHaskell.Auth.JwksCache (httpFetchViaClient, newJwksCache)
import BackendHaskell.Auth.JwksVerifier (newJwksVerifier)
import MockJwksServer
import TestTokenHelper

keycloakIssuer, audience, hmacSecret, expectedClientId :: T.Text
keycloakIssuer = "http://localhost:8082/realms/training"
audience = "backend"
hmacSecret = "local-dev-hmac-shared-secret-change-me"
expectedClientId = "external-api-client"

spec :: Spec
spec = around withMockJwksServer $ describe "requireExternalClient" $ do
  it "accepts a Keycloak-issued token with matching azp" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-1" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    let dispatcher = mkDispatcher cache
    token <- makeRsaTokenWithAzp priv "kid-1" keycloakIssuer audience "sub-1" 3600 expectedClientId
    result <- requireExternalClient dispatcher expectedClientId (Just ("Bearer " <> token))
    result `shouldBe` Right ()

  it "rejects a valid token with a missing azp" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-2" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    let dispatcher = mkDispatcher cache
    token <- makeRsaToken priv "kid-2" keycloakIssuer audience "sub-1" 3600
    result <- requireExternalClient dispatcher expectedClientId (Just ("Bearer " <> token))
    result `shouldSatisfy` isLeft

  it "rejects a valid token with a wrong azp" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-3" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    let dispatcher = mkDispatcher cache
    token <- makeRsaTokenWithAzp priv "kid-3" keycloakIssuer audience "sub-1" 3600 "some-other-client"
    result <- requireExternalClient dispatcher expectedClientId (Just ("Bearer " <> token))
    result `shouldSatisfy` isLeft

  it "rejects a correctly-signed local HMAC token even with the correct azp" $ \(_mock, port) -> do
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    let dispatcher = mkDispatcher cache
    token <- makeHmacTokenWithAzp hmacSecret localHmacIssuer audience "1" 3600 expectedClientId
    result <- requireExternalClient dispatcher expectedClientId (Just ("Bearer " <> token))
    result `shouldSatisfy` isLeft

  it "rejects a missing Authorization header" $ \(_mock, _port) -> do
    cache <- newJwksCache "http://127.0.0.1:1/jwks" httpFetchViaClient
    let dispatcher = mkDispatcher cache
    result <- requireExternalClient dispatcher expectedClientId Nothing
    result `shouldSatisfy` isLeft
  where
    mkDispatcher cache =
      registerVerifier keycloakIssuer (newJwksVerifier cache keycloakIssuer audience)
        $ registerVerifier localHmacIssuer (newHmacVerifier hmacSecret localHmacIssuer audience)
        $ newDispatcher

jwksUrlOf :: Int -> T.Text
jwksUrlOf port = T.pack ("http://127.0.0.1:" ++ show port ++ "/jwks")

isLeft :: Either a b -> Bool
isLeft (Left _) = True
isLeft _ = False
