module ValidationSpec (spec) where

import Data.Time (Day, fromGregorian)
import qualified Data.Text as T
import Test.Hspec

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Domain.Models (TaskInput (..))
import BackendHaskell.Domain.Validation (parseFinishedOn, validateTaskInput)

today :: Day
today = fromGregorian 2026 6 15

spec :: Spec
spec = do
  describe "validateTaskInput" $ do
    it "accepts valid input" $
      validateTaskInput (mkInput id) today `shouldBe` Right (1, "waiting")

    it "rejects empty name" $
      isValidationError (validateTaskInput (mkInput (\i -> i {tiName = ""})) today)

    it "rejects name over 20 codepoints" $
      isValidationError (validateTaskInput (mkInput (\i -> i {tiName = T.replicate 21 "a"})) today)

    it "accepts name exactly 20 codepoints" $
      case validateTaskInput (mkInput (\i -> i {tiName = T.replicate 20 "a"})) today of
        Right _ -> pure ()
        Left e -> expectationFailure (show e)

    it "accepts astral emoji name of 20 codepoints" $
      case validateTaskInput (mkInput (\i -> i {tiName = T.replicate 20 "\128512"})) today of
        Right _ -> pure ()
        Left e -> expectationFailure (show e)

    it "rejects astral emoji name of 21 codepoints" $
      isValidationError (validateTaskInput (mkInput (\i -> i {tiName = T.replicate 21 "\128512"})) today)

    it "rejects past finished_on" $
      isValidationError (validateTaskInput (mkInput (\i -> i {tiFinishedOn = pred today})) today)

    it "accepts finished_on equal to today" $
      case validateTaskInput (mkInput id) today of
        Right _ -> pure ()
        Left e -> expectationFailure (show e)

    it "rejects unknown status" $
      isValidationError (validateTaskInput (mkInput (\i -> i {tiStatusRaw = "bogus"})) today)

  describe "parseFinishedOn" $ do
    it "rejects calendar-invalid dates" $
      case parseFinishedOn "2026-02-30" of
        Left (TaskError InvalidFinishedOn _) -> pure ()
        other -> expectationFailure ("expected InvalidFinishedOn, got " ++ show (either (const "Left") (const "Right") other))

    it "accepts a leap-year date" $
      parseFinishedOn "2024-02-29" `shouldSatisfy` isRight

isRight :: Either a b -> Bool
isRight (Right _) = True
isRight _ = False

isValidationError :: Show a => Either TaskError a -> Expectation
isValidationError (Left (TaskError Validation _)) = pure ()
isValidationError other = expectationFailure ("expected Validation error, got: " ++ show other)

mkInput :: (TaskInput -> TaskInput) -> TaskInput
mkInput f =
  f
    TaskInput
      { tiName = "test"
      , tiDescription = Nothing
      , tiStatusRaw = "waiting"
      , tiFinishedOn = today
      , tiLabelIds = []
      }
