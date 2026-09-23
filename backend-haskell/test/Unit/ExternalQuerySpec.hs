module ExternalQuerySpec (spec) where

import Test.Hspec

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.External.Query
  ( cursorAfterId
  , cursorLimit
  , offsetPage
  , offsetPageSize
  , parseUserId
  )

spec :: Spec
spec = do
  describe "parseUserId" $ do
    it "rejects missing user_id" $
      parseUserId Nothing `shouldBe` Left (TaskError InvalidRequest "user_id_required")

    it "rejects empty user_id" $
      parseUserId (Just "") `shouldBe` Left (TaskError InvalidRequest "user_id_required")

    it "rejects non-numeric user_id" $
      parseUserId (Just "abc") `shouldBe` Left (TaskError InvalidRequest "invalid_user_id")

    it "accepts numeric user_id" $
      parseUserId (Just "42") `shouldBe` Right 42

  describe "offsetPage" $ do
    it "defaults to 1 when missing" $ offsetPage Nothing `shouldBe` 1
    it "clamps 0 up to 1" $ offsetPage (Just 0) `shouldBe` 1
    it "clamps negative values up to 1" $ offsetPage (Just (-5)) `shouldBe` 1
    it "passes through positive values" $ offsetPage (Just 3) `shouldBe` 3

  describe "offsetPageSize" $ do
    it "defaults to 10 when missing" $ offsetPageSize Nothing `shouldBe` 10
    it "clamps 0 up to 1" $ offsetPageSize (Just 0) `shouldBe` 1
    it "passes through positive values" $ offsetPageSize (Just 25) `shouldBe` 25

  describe "cursorAfterId" $ do
    it "defaults to 0 when missing" $ cursorAfterId Nothing `shouldBe` 0
    it "treats non-numeric cursor as 0 (start from beginning)" $ cursorAfterId (Just "not-a-number") `shouldBe` 0
    it "treats 0 or negative cursor as 0" $ cursorAfterId (Just "-1") `shouldBe` 0
    it "passes through positive cursor" $ cursorAfterId (Just "383") `shouldBe` 383

  describe "cursorLimit" $ do
    it "defaults to 10 when missing" $ cursorLimit Nothing `shouldBe` 10
    it "clamps 0 up to 1" $ cursorLimit (Just 0) `shouldBe` 1
    it "passes through positive values" $ cursorLimit (Just 5) `shouldBe` 5
