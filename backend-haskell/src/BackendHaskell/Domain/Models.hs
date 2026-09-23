module BackendHaskell.Domain.Models
  ( TaskStatus (..)
  , statusFromWireValue
  , statusFromDbValue
  , Label (..)
  , User (..)
  , TaskInput (..)
  , Task (..)
  ) where

import Data.Text (Text)
import Data.Time (Day, LocalTime)

-- | backend(Go)のTaskStatus(waiting=1, work_in_progress=2, completed=3)と同じマッピング
data TaskStatus = Waiting | WorkInProgress | Completed
  deriving (Eq, Show)

statusFromWireValue :: Text -> Maybe (Int, TaskStatus, Text)
statusFromWireValue "waiting" = Just (1, Waiting, "waiting")
statusFromWireValue "work_in_progress" = Just (2, WorkInProgress, "work_in_progress")
statusFromWireValue "completed" = Just (3, Completed, "completed")
statusFromWireValue _ = Nothing

statusFromDbValue :: Int -> Maybe (Int, TaskStatus, Text)
statusFromDbValue 1 = Just (1, Waiting, "waiting")
statusFromDbValue 2 = Just (2, WorkInProgress, "work_in_progress")
statusFromDbValue 3 = Just (3, Completed, "completed")
statusFromDbValue _ = Nothing

data Label = Label
  { labelId :: Int
  , labelName :: Text
  }
  deriving (Eq, Show)

data User = User
  { userId :: Int
  , userEmail :: Text
  , userName :: Text
  }
  deriving (Eq, Show)

data TaskInput = TaskInput
  { tiName :: Text
  , tiDescription :: Maybe Text
  , tiStatusRaw :: Text
  , tiFinishedOn :: Day
  , tiLabelIds :: [Int]
  }
  deriving (Eq, Show)

-- | user_idはワイヤーに乗せない(REST/gRPCとも)。backend(Go)実装がtaskDTOToJSONでuser_idを
-- 含めていない実際の挙動に合わせている(CONTRACT.mdセクション5.1本文の例には書かれているが、
-- ワイヤー契約パリティの原則(セクション20.5)により、ドキュメントではなく実際の挙動に合わせる。
-- backend-java/backend-kotlin/backend-python/backend-elixir/backend-rust/backend-c/backend-cppの
-- 全てで同じ既知の差異が確認・踏襲されている)
data Task = Task
  { taskId :: Int
  , taskName :: Text
  , taskDescription :: Maybe Text
  , taskStatusDb :: Int
  , taskStatusWire :: Text
  , taskFinishedOn :: Day
  , taskUserId :: Int
  , taskLabels :: [Label]
  -- 【実機検証で確認する必要がある点、README.md「実装時に判明した既知の差異」参照】
  -- mysql-simpleがDATETIME列をどう返すか(タイムゾーン変換の有無)は、backend-java(JDBCで
  -- システムデフォルトタイムゾーン経由の変換バグが実際に見つかった)・backend-kotlin/
  -- backend-python/backend-elixir(ドライバがナイーブな値をそのまま返すため変換バグが無い)と
  -- 同じ観点で実機検証が必要。LocalTime(タイムゾーン情報を持たない、Haskellの`time`パッケージの
  -- 型)を使うことで、MySQLのDATETIME列(こちらもタイムゾーン情報を持たない)の値をそのまま
  -- 保持する設計にしている
  , taskCreatedAt :: LocalTime
  , taskUpdatedAt :: LocalTime
  }
  deriving (Eq, Show)
