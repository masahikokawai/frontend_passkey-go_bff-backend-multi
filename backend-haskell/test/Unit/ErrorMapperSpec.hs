module ErrorMapperSpec (spec) where

import Data.Aeson (object, (.=))
import Test.Hspec

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Rest.ErrorMapper (statusAndBody)

spec :: Spec
spec = describe "statusAndBody" $ do
  it "unauthorized maps to 401" $
    statusAndBody (TaskError Unauthorized "unauthorized") `shouldBe` (401, object ["error" .= ("unauthorized" :: String)])

  it "user_not_provisioned maps to 403" $
    statusAndBody (TaskError UserNotProvisioned "user_not_provisioned")
      `shouldBe` (403, object ["error" .= ("user_not_provisioned" :: String)])

  it "invalid_request maps to 400" $
    statusAndBody (TaskError InvalidRequest "invalid_request") `shouldBe` (400, object ["error" .= ("invalid_request" :: String)])

  it "invalid_id maps to 400" $
    statusAndBody (TaskError InvalidId "invalid_id") `shouldBe` (400, object ["error" .= ("invalid_id" :: String)])

  it "invalid_status maps to 422" $
    statusAndBody (TaskError InvalidStatus "invalid_status") `shouldBe` (422, object ["error" .= ("invalid_status" :: String)])

  it "invalid_finished_on maps to 422" $
    statusAndBody (TaskError InvalidFinishedOn "invalid_finished_on")
      `shouldBe` (422, object ["error" .= ("invalid_finished_on" :: String)])

  it "validation maps to 422 with message" $
    statusAndBody (TaskError Validation "nameは必須です")
      `shouldBe` (422, object ["error" .= ("validation_error" :: String), "message" .= ("nameは必須です" :: String)])

  it "not_found maps to 404" $
    statusAndBody (TaskError NotFound "not_found") `shouldBe` (404, object ["error" .= ("not_found" :: String)])

  it "internal maps to 500" $
    statusAndBody (TaskError Internal "boom") `shouldBe` (500, object ["error" .= ("internal_server_error" :: String)])
