-- | backend(Go)のservice.validateTaskInputと同じルール:
--   - name: 必須・20コードポイント以内
--   - finished_on: 過去日不可(UTC基準の「今日」)
--   - status: enumの範囲内
--
-- このプロジェクトは全言語で一貫して「ビジネスルールの検証を自前実装する」方針を貫いている。
-- Servantの型駆動なリクエスト解析(JSON->TaskInput相当の構造的な型変換)とは別に、
-- ここでのビジネスルール検証は他言語のTaskValidation/validate_task_input相当と同じ、
-- 明示的な関数として書く(README.md「アーキテクチャ選定」節参照)。
--
-- 【Haskell固有の利点、他言語との対比】Haskellの`Char`型はUnicodeコードポイントそのものを
-- 表す(Java/KotlinのようにUTF-16コード単位ではない)ため、`Data.Text.length`は
-- 何もしなくても最初からコードポイント数を返す。これはPythonの`len()`が最初から
-- コードポイントを返すのと同じ「何もしなくて良い」ケースであり、Java/KotlinのUTF-16
-- サロゲートペア二重カウント問題や、Elixirの`String.length/1`が既定で書記素クラスタを
-- 数えてしまう問題は、Haskellでは最初から起こり得ない。
module BackendHaskell.Domain.Validation
  ( validateTaskInput
  , parseFinishedOn
  ) where

import Data.Text (Text)
import qualified Data.Text as T
import Data.Time (Day)
import Data.Time.Calendar (fromGregorianValid)
import Text.Read (readMaybe)

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Domain.Models (TaskInput (..), statusFromWireValue)

maxNameCodepoints :: Int
maxNameCodepoints = 20

validation :: Text -> TaskError
validation = TaskError Validation

validateTaskInput :: TaskInput -> Day -> Either TaskError (Int, Text)
validateTaskInput input today = do
  validateName (tiName input)
  validateFinishedOn (tiFinishedOn input) today
  case statusFromWireValue (tiStatusRaw input) of
    Just (db, _, wire) -> Right (db, wire)
    Nothing -> Left (validation ("不明なstatus: \"" <> tiStatusRaw input <> "\""))

validateName :: Text -> Either TaskError ()
validateName name
  | T.null name = Left (validation "nameは必須です")
  | T.length name > maxNameCodepoints = Left (validation "nameは20文字以内である必要があります")
  | otherwise = Right ()

validateFinishedOn :: Day -> Day -> Either TaskError ()
validateFinishedOn finishedOn today
  | finishedOn < today = Left (validation "finished_onに過去日は指定できません")
  | otherwise = Right ()

-- | カレンダー上の妥当性まで検証する(例: "2026-02-30"は文字列としては整形式だが実在しない日付)。
-- `fromGregorianValid`はこれを`Nothing`で拒否するため、`InvalidFinishedOn`にマッピングする
-- (backend-pythonのparse_finished_on/backend-kotlinのLocalDate.parseと同じ役割の分離:
-- 「形式として壊れている」はinvalid_finished_on、「形式は正しいが過去日」はvalidation_error)
parseFinishedOn :: Text -> Either TaskError Day
parseFinishedOn raw =
  case T.splitOn "-" raw of
    [yRaw, mRaw, dRaw] ->
      case (readMaybe (T.unpack yRaw), readMaybe (T.unpack mRaw), readMaybe (T.unpack dRaw)) of
        (Just y, Just m, Just d) ->
          case fromGregorianValid y m d of
            Just day -> Right day
            Nothing -> Left (TaskError InvalidFinishedOn "invalid_finished_on")
        _ -> Left (TaskError InvalidFinishedOn "invalid_finished_on")
    _ -> Left (TaskError InvalidFinishedOn "invalid_finished_on")
