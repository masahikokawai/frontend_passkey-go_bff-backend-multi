module JwksVerifierSpec (spec) where

import qualified Data.Text as T
import Test.Hspec

import BackendHaskell.Auth.Claims (Claims (..))
import BackendHaskell.Auth.JwksCache (httpFetchViaClient, newJwksCache)
import BackendHaskell.Auth.JwksVerifier (newJwksVerifier)
import MockJwksServer
import TestTokenHelper

issuer, audience :: T.Text
issuer = "http://localhost:8082/realms/training"
audience = "backend"

spec :: Spec
spec = around withMockJwksServer $ describe "JwksVerifier" $ do
  it "accepts valid token signed with known key" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-1" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    token <- makeRsaToken priv "kid-1" issuer audience "99" 3600
    let verifier = newJwksVerifier cache issuer audience
    result <- verifier token
    case result of
      Right claims -> claimsSub claims `shouldBe` "99"
      Left err -> expectationFailure (show err)

  it "unknown kid triggers refresh then succeeds if now present" $ \(mock, port) -> do
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    let verifier = newJwksVerifier cache issuer audience
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    token <- makeRsaToken priv "kid-late" issuer audience "5" 3600
    addKey mock "kid-late" (n, e)
    result <- verifier token
    case result of
      Right claims -> claimsSub claims `shouldBe` "5"
      Left err -> expectationFailure (show err)

  it "unknown kid still unknown after refresh is rejected" $ \(mock, port) -> do
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    let verifier = newJwksVerifier cache issuer audience
    (_, priv) <- generateRsaKeyPair
    token <- makeRsaToken priv "kid-never-registered" issuer audience "1" 3600
    result <- verifier token
    result `shouldSatisfy` isLeft

  it "wrong issuer is rejected even with valid signature" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-2" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    token <- makeRsaToken priv "kid-2" "unexpected-issuer" audience "1" 3600
    let verifier = newJwksVerifier cache issuer audience
    result <- verifier token
    result `shouldSatisfy` isLeft

  it "wrong audience is rejected" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-3" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    token <- makeRsaToken priv "kid-3" issuer "someone-else" "1" 3600
    let verifier = newJwksVerifier cache issuer audience
    result <- verifier token
    result `shouldSatisfy` isLeft

  it "expired token is rejected" $ \(mock, port) -> do
    (pub, priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-4" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    token <- makeRsaToken priv "kid-4" issuer audience "1" (-3600)
    let verifier = newJwksVerifier cache issuer audience
    result <- verifier token
    result `shouldSatisfy` isLeft

  it "rejects unexpected algorithm" $ \(mock, port) -> do
    (pub, _priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-5" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    -- RS256を期待しているVerifierに対し、HS256の(全く別の鍵体系の)トークンを送る
    hmacToken <- makeHmacToken "irrelevant-secret-that-is-at-least-32-bytes" issuer audience "1" 3600
    let verifier = newJwksVerifier cache issuer audience
    result <- verifier hmacToken
    result `shouldSatisfy` isLeft

jwksUrlOf :: Int -> T.Text
jwksUrlOf port = T.pack ("http://127.0.0.1:" ++ show port ++ "/jwks")

isLeft :: Either a b -> Bool
isLeft (Left _) = True
isLeft _ = False
