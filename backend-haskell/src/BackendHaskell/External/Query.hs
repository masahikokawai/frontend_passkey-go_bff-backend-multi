-- | 外部公開API(/external/v1/tasks)のクエリパラメータ解析。DB・HTTPを一切介さない
-- 純粋関数として分離しており、単体テストで直接検証できる(backend-c/backend-cppの
-- external_query相当)
module BackendHaskell.External.Query
  ( parseUserId
  , offsetPage
  , offsetPageSize
  , cursorAfterId
  , cursorLimit
  ) where

import Data.Text (Text)
import qualified Data.Text as T
import Text.Read (readMaybe)

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (InvalidRequest))

-- | user_idは必須。欠落/空文字はuser_id_required、数値でなければinvalid_user_id
parseUserId :: Maybe Text -> Either TaskError Int
parseUserId Nothing = Left (TaskError InvalidRequest "user_id_required")
parseUserId (Just raw)
  | T.null raw = Left (TaskError InvalidRequest "user_id_required")
  | otherwise = case readMaybe (T.unpack raw) :: Maybe Int of
      Just n -> Right n
      Nothing -> Left (TaskError InvalidRequest "invalid_user_id")

-- | pageは1未満なら1にクランプする(既定1)
offsetPage :: Maybe Int -> Int
offsetPage = clampAtLeast1 . maybe 1 id

-- | page_sizeは1未満なら1にクランプする(既定10)
offsetPageSize :: Maybe Int -> Int
offsetPageSize = clampAtLeast1 . maybe 10 id

-- | cursor省略時は先頭から(after_id=0扱い)。0以下は「未指定」として扱う
cursorAfterId :: Maybe Text -> Int
cursorAfterId Nothing = 0
cursorAfterId (Just raw) = maybe 0 (\n -> if n > 0 then n else 0) (readMaybe (T.unpack raw) :: Maybe Int)

-- | limitは1未満なら1にクランプする(既定10)
cursorLimit :: Maybe Int -> Int
cursorLimit = clampAtLeast1 . maybe 10 id

clampAtLeast1 :: Int -> Int
clampAtLeast1 n = max 1 n
