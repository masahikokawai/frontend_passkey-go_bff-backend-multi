-- | Domain.Task <-> Task.V1.Task(生成されたprotobufモジュール)の相互変換
module BackendHaskell.Grpc.ProtoMapper
  ( toProto
  , fromCreateRequest
  , fromUpdateRequest
  ) where

import Control.Lens ((&), (.~), (^.))
import Data.ProtoLens (defMessage)
import Data.ProtoLens.Labels ()
import qualified Data.Text as T
import Data.Time (Day, LocalTime (..), UTCTime (..), timeOfDayToTime)
import Data.Time.Clock.POSIX (utcTimeToPOSIXSeconds)
import Data.Time.Format (defaultTimeLocale, formatTime)

import BackendHaskell.Domain.Errors (TaskError (..))
import qualified BackendHaskell.Domain.Models as Dom
import BackendHaskell.Domain.Models (TaskInput (..))
import BackendHaskell.Domain.Validation (parseFinishedOn)
import qualified Proto.Google.Protobuf.Timestamp as PT
import Proto.Task.V1.Task
import Proto.Task.V1.Task_Fields

toProto :: Dom.Task -> Proto.Task.V1.Task.Task
toProto t =
  base
    & #id .~ fromIntegral (Dom.taskId t)
    & #name .~ Dom.taskName t
    & #status .~ Dom.taskStatusWire t
    & #finishedOn .~ dayToIso8601 (Dom.taskFinishedOn t)
    & #labels .~ map labelToProto (Dom.taskLabels t)
    & #createdAt .~ toTimestamp (Dom.taskCreatedAt t)
    & #updatedAt .~ toTimestamp (Dom.taskUpdatedAt t)
    & maybeSetDescription (Dom.taskDescription t)
  where
    base = defMessage :: Proto.Task.V1.Task.Task
    maybeSetDescription Nothing task = task
    maybeSetDescription (Just d) task = task & #description .~ d

labelToProto :: Dom.Label -> Proto.Task.V1.Task.Label
labelToProto l = defMessage & #id .~ fromIntegral (Dom.labelId l) & #name .~ Dom.labelName l

fromCreateRequest :: CreateTaskRequest -> Either TaskError TaskInput
fromCreateRequest req = do
  finishedOn <- parseFinishedOn (req ^. #finishedOn)
  Right
    TaskInput
      { tiName = req ^. #name
      , tiDescription = if req ^. #maybe'description == Nothing then Nothing else Just (req ^. #description)
      , tiStatusRaw = req ^. #status
      , tiFinishedOn = finishedOn
      , tiLabelIds = map fromIntegral (req ^. #labelIds)
      }

fromUpdateRequest :: UpdateTaskRequest -> Either TaskError TaskInput
fromUpdateRequest req = do
  finishedOn <- parseFinishedOn (req ^. #finishedOn)
  Right
    TaskInput
      { tiName = req ^. #name
      , tiDescription = if req ^. #maybe'description == Nothing then Nothing else Just (req ^. #description)
      , tiStatusRaw = req ^. #status
      , tiFinishedOn = finishedOn
      , tiLabelIds = map fromIntegral (req ^. #labelIds)
      }

dayToIso8601 :: Day -> T.Text
dayToIso8601 d = T.pack (formatTime defaultTimeLocale "%Y-%m-%d" d)

-- 【実機検証で確認する必要がある点、backend-python(FromDatetime、ナイーブなdatetimeをUTCとして
-- 扱う)/backend-java(JDBCのタイムゾーン変換バグ)/backend-elixir(NaiveDateTime->DateTime.from_naive!)
-- と同じ観点】LocalTimeはUTCの壁時計値そのものであることを前提に、明示的にUTC epoch秒へ
-- 変換してからgoogle.protobuf.Timestampへ渡す(タイムゾーン変換は一切発生しない)
toTimestamp :: LocalTime -> PT.Timestamp
toTimestamp lt = defMessage & #seconds .~ epochSeconds & #nanos .~ 0
  where
    -- LocalTime(タイムゾーン情報を持たない、UTCの壁時計値そのもの)を、実際のタイムゾーン
    -- 変換を一切行わずUTCTimeへそのまま組み直す(Day + TimeOfDayの構成要素をそのまま
    -- UTCTimeのDay + DiffTimeへ詰め替えるだけ)
    asUtc = UTCTime (localDay lt) (timeOfDayToTime (localTimeOfDay lt))
    epochSeconds = floor (utcTimeToPOSIXSeconds asUtc)
