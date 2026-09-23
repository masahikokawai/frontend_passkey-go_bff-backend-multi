-- | CONTRACT.mdセクション5.1のJSON形状(スネークケース)。
-- 【backend(Go)の実際の挙動に合わせた既知の差異】taskDTOToJSON(backend/internal/handler/v1/task.go)は
-- user_idをレスポンスに含めていない(CONTRACT.md本文の例には書かれているが、実装はそうなっていない。
-- ワイヤー契約パリティの原則(セクション20.5)に従い、ドキュメントではなく実際の挙動に合わせる。
-- backend-java/backend-kotlin/backend-python/backend-elixir/backend-rust/backend-c/backend-cppの
-- 全てで同じ既知の差異が確認・踏襲されている)
module BackendHaskell.Rest.TaskJson
  ( taskToJson
  ) where

import Data.Aeson (Value (..), object, (.=))
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time (Day, LocalTime)
import Data.Time.Format (defaultTimeLocale, formatTime)

import BackendHaskell.Domain.Models (Label (..), Task (..))

taskToJson :: Task -> Value
taskToJson t =
  object
    [ "id" .= taskId t
    , "name" .= taskName t
    , "description" .= maybe Null String (taskDescription t)
    , "status" .= taskStatusWire t
    , "finished_on" .= dayToIso8601 (taskFinishedOn t)
    , "labels" .= map labelToJson (taskLabels t)
    , "created_at" .= toRfc3339 (taskCreatedAt t)
    , "updated_at" .= toRfc3339 (taskUpdatedAt t)
    ]

labelToJson :: Label -> Value
labelToJson l = object ["id" .= labelId l, "name" .= labelName l]

dayToIso8601 :: Day -> Text
dayToIso8601 d = T.pack (formatTime defaultTimeLocale "%Y-%m-%d" d)

toRfc3339 :: LocalTime -> Text
toRfc3339 t = T.pack (formatTime defaultTimeLocale "%Y-%m-%dT%H:%M:%S" t) <> "+00:00"
