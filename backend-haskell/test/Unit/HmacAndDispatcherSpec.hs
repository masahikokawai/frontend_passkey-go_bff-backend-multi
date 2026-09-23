module HmacAndDispatcherSpec (spec) where

import Test.Hspec

import BackendHaskell.Auth.Claims (Claims (..))
import BackendHaskell.Auth.Dispatcher
import BackendHaskell.Auth.HmacVerifier (newHmacVerifier)
import TestTokenHelper (makeAlgNoneToken, makeHmacToken)

spec :: Spec
spec = do
  describe "isLocalIssuer" $
    it "matches hmac and rsa only" $ do
      isLocalIssuer localHmacIssuer `shouldBe` True
      isLocalIssuer localRsaIssuer `shouldBe` True
      isLocalIssuer "http://localhost:8082/realms/training" `shouldBe` False
      isLocalIssuer "" `shouldBe` False

  describe "HmacVerifier" $ do
    it "accepts valid token" $ do
      token <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend" "42" 3600
      let verifier = newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend"
      result <- verifier token
      case result of
        Right claims -> do
          claimsSub claims `shouldBe` "42"
          claimsIss claims `shouldBe` localHmacIssuer
        Left err -> expectationFailure (show err)

    it "rejects wrong secret" $ do
      token <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend" "42" 3600
      let verifier = newHmacVerifier "different-secret-that-is-at-least-32-bytes" localHmacIssuer "backend"
      result <- verifier token
      result `shouldSatisfy` isLeft

    it "rejects expired token" $ do
      token <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend" "42" (-3600)
      let verifier = newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend"
      result <- verifier token
      result `shouldSatisfy` isLeft

    it "rejects wrong audience" $ do
      token <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "someone-else" "42" 3600
      let verifier = newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend"
      result <- verifier token
      result `shouldSatisfy` isLeft

    it "rejects wrong issuer" $ do
      token <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" "unexpected-issuer" "backend" "42" 3600
      let verifier = newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend"
      result <- verifier token
      result `shouldSatisfy` isLeft

    it "rejects alg none" $ do
      token <- makeAlgNoneToken localHmacIssuer "backend" "42" 3600
      let verifier = newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend"
      result <- verifier token
      result `shouldSatisfy` isLeft

  describe "Dispatcher" $ do
    it "routes by issuer and rejects unknown issuer" $ do
      let dispatcher = registerVerifier localHmacIssuer (newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend") newDispatcher
      goodToken <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend" "7" 3600
      goodResult <- dispatchVerify dispatcher goodToken
      case goodResult of
        Right claims -> claimsSub claims `shouldBe` "7"
        Left err -> expectationFailure (show err)

      unknownToken <- makeHmacToken "test-secret-that-is-at-least-32-bytes-long" "unknown-issuer" "backend" "7" 3600
      unknownResult <- dispatchVerify dispatcher unknownToken
      unknownResult `shouldBe` Left "unknown_issuer"

    it "rejects malformed token" $ do
      let dispatcher = registerVerifier localHmacIssuer (newHmacVerifier "test-secret-that-is-at-least-32-bytes-long" localHmacIssuer "backend") newDispatcher
      result <- dispatchVerify dispatcher "not-a-jwt"
      result `shouldBe` Left "malformed_token"

isLeft :: Either a b -> Bool
isLeft (Left _) = True
isLeft _ = False
