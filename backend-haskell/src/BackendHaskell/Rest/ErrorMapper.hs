-- | backend-java/backend-kotlin/backend-python/backend-elixirのErrorMapper/status_and_bodyと
-- 1文字も変えていないJSON形状・HTTPステータス(CONTRACT.mdセクション20.5のワイヤー契約パリティ)
module BackendHaskell.Rest.ErrorMapper
  ( statusAndBody
  ) where

import Data.Aeson (Value, object, (.=))
import Data.Text (Text)

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))

statusAndBody :: TaskError -> (Int, Value)
statusAndBody (TaskError Validation message) = (422, object ["error" .= ("validation_error" :: Text), "message" .= message])
statusAndBody (TaskError kind _) = kindToStatusAndError kind

kindToStatusAndError :: TaskErrorKind -> (Int, Value)
kindToStatusAndError Unauthorized = (401, object ["error" .= ("unauthorized" :: Text)])
kindToStatusAndError UserNotProvisioned = (403, object ["error" .= ("user_not_provisioned" :: Text)])
kindToStatusAndError InvalidRequest = (400, object ["error" .= ("invalid_request" :: Text)])
kindToStatusAndError InvalidId = (400, object ["error" .= ("invalid_id" :: Text)])
kindToStatusAndError InvalidStatus = (422, object ["error" .= ("invalid_status" :: Text)])
kindToStatusAndError InvalidFinishedOn = (422, object ["error" .= ("invalid_finished_on" :: Text)])
kindToStatusAndError NotFound = (404, object ["error" .= ("not_found" :: Text)])
kindToStatusAndError Internal = (500, object ["error" .= ("internal_server_error" :: Text)])
kindToStatusAndError Validation = (422, object ["error" .= ("validation_error" :: Text)])
