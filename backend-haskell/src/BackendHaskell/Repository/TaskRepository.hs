-- | 生SQL + mysql-haskellのみ(ORM禁止方針、他11言語(Rails/backend-elixirのEcto採用は
-- 意図的な例外)と統一)。ビジネスルールの検証はDomain.Validationが担い、ここはDBアクセスに
-- 専念する。
--
-- 【この実装で最も学習価値の高い設計判断、backend-scala-http4s/TaskRepo.scalaのIO[...]と
-- 対になるコメント】このモジュールの全ての公開関数は`IO`アクションを返す。Haskellでは
-- 副作用(DBへのネットワークI/O)を行う関数の型に必ず`IO`が現れ、コンパイラがこれを
-- 強制する。IOを経由しない限り副作用のあるコードを呼び出すこと自体ができない
-- (このIOはServant/Handlerやgrapesyのハンドラの型にもそのまま伝播し、REST/gRPCの
-- どちらのハンドラも最終的にはIOアクションになる)。これは、同じくJVM上に実装済みの
-- backend-scala-http4s(cats-effectの`IO`)が全く同じ役割を果たしているのと概念的に同じ
-- 発想である(backend-scala-http4s/src/main/scala/com/bffgin/backend/TaskRepo.scalaの
-- `listOffset`/`get`/`create`/`update`/`delete`等、全て`IO[...]`型を持つ箇所と対比、
-- 詳細はREADME.md「アーキテクチャ選定」節のIOモナドに関する節を参照)。何が同じで
-- 何が違うかもそちらに記載している(遅延評価の有無、言語組み込みのRTSプリミティブか
-- サードパーティのエフェクトシステムライブラリかという違いなど)。
module BackendHaskell.Repository.TaskRepository
  ( listOffset
  , listCursor
  , findById
  , create
  , update
  , delete
  , findUserById
  , findUserByKeycloakSub
  ) where

import Data.List (nub)
import Data.Pool (Pool, withResource)
import qualified Data.String as S
import Data.Text (Text)
import qualified Data.Text.Encoding as TE
import Data.Time (Day, LocalTime, getCurrentTime, utcToLocalTime, utc)
import Database.MySQL.Base
import qualified System.IO.Streams as Streams

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Domain.Models (Label (..), Task (..), TaskInput (..), User (..))

withConn :: Pool MySQLConn -> (MySQLConn -> IO a) -> IO a
withConn = withResource

nowLocal :: IO LocalTime
nowLocal = utcToLocalTime utc <$> getCurrentTime

-- ===== 値変換ヘルパー(mysql-haskellはFromRow相当の自動導出を提供しないため、
-- MySQLValueから素朴にパターンマッチで取り出す。これも「ORM禁止・生SQL」の一貫として
-- 意図的に手書きする) =====

asInt :: MySQLValue -> Int
asInt (MySQLInt8 v) = fromIntegral v
asInt (MySQLInt8U v) = fromIntegral v
asInt (MySQLInt16 v) = fromIntegral v
asInt (MySQLInt16U v) = fromIntegral v
asInt (MySQLInt32 v) = fromIntegral v
asInt (MySQLInt32U v) = fromIntegral v
asInt (MySQLInt64 v) = fromIntegral v
asInt (MySQLInt64U v) = fromIntegral v
asInt v = error ("TaskRepository: expected integer MySQLValue, got: " ++ show v)

asText :: MySQLValue -> Text
asText (MySQLText t) = t
asText (MySQLBytes b) = TE.decodeUtf8 b
asText v = error ("TaskRepository: expected text MySQLValue, got: " ++ show v)

asMaybeText :: MySQLValue -> Maybe Text
asMaybeText MySQLNull = Nothing
asMaybeText v = Just (asText v)

asDay :: MySQLValue -> Day
asDay (MySQLDate d) = d
asDay v = error ("TaskRepository: expected date MySQLValue, got: " ++ show v)

asLocalTime :: MySQLValue -> LocalTime
asLocalTime (MySQLDateTime t) = t
asLocalTime (MySQLTimeStamp t) = t
asLocalTime v = error ("TaskRepository: expected datetime MySQLValue, got: " ++ show v)

statusWireOf :: Int -> Text
statusWireOf 1 = "waiting"
statusWireOf 2 = "work_in_progress"
statusWireOf 3 = "completed"
statusWireOf _ = "waiting"

rowToTaskBase :: [MySQLValue] -> [Label] -> Task
rowToTaskBase [idV, nameV, descV, statusV, finishedV, userIdV, createdV, updatedV] labels =
  Task
    { taskId = asInt idV
    , taskName = asText nameV
    , taskDescription = asMaybeText descV
    , taskStatusDb = asInt statusV
    , taskStatusWire = statusWireOf (asInt statusV)
    , taskFinishedOn = asDay finishedV
    , taskUserId = asInt userIdV
    , taskLabels = labels
    , taskCreatedAt = asLocalTime createdV
    , taskUpdatedAt = asLocalTime updatedV
    }
rowToTaskBase row _ = error ("TaskRepository: unexpected row shape: " ++ show row)

taskColumns :: Query
taskColumns = "id, name, description, status, finished_on, user_id, created_at, updated_at"

fetchRows :: MySQLConn -> Query -> [MySQLValue] -> IO [[MySQLValue]]
fetchRows conn q params = do
  (_, is) <- query conn q params
  Streams.toList is

attachLabelsToRows :: MySQLConn -> [[MySQLValue]] -> IO [Task]
attachLabelsToRows _ [] = pure []
attachLabelsToRows conn rows = do
  let ids = map (\r -> asInt (head r)) rows
      placeholders = intercalateComma (map (const ("?" :: String)) ids)
  labelRows <-
    fetchRows
      conn
      ( S.fromString
          ( "SELECT task_labels.task_id, labels.id, labels.name FROM task_labels "
              <> "JOIN labels ON labels.id = task_labels.label_id "
              <> "WHERE task_labels.task_id IN (" <> placeholders <> ")"
          )
      )
      (map (MySQLInt64 . fromIntegral) ids)
  let labelsFor tid = [Label {labelId = asInt lid, labelName = asText lname} | [tidV, lid, lname] <- labelRows, asInt tidV == tid]
  pure [rowToTaskBase r (labelsFor (asInt (head r))) | r <- rows]

intercalateComma :: [String] -> String
intercalateComma [] = ""
intercalateComma [x] = x
intercalateComma (x : xs) = x ++ "," ++ intercalateComma xs

-- | v1(REST)向け: offsetページング
listOffset :: Pool MySQLConn -> Int -> Int -> Int -> IO ([Task], Int)
listOffset pool userId limit offset = withConn pool $ \conn -> do
  countRows <- fetchRows conn "SELECT COUNT(*) FROM tasks WHERE user_id = ?" [MySQLInt64 (fromIntegral userId)]
  let total = case countRows of
        [[c]] -> asInt c
        _ -> 0
  -- 【他言語と同じtie-break、backend-c/backend-cppで見つかった既知バグの再発防止】
  -- created_at同値(DATETIME列は秒精度)だけでは並び順が不定になるため、idを追加のtie-breakとする
  rows <-
    fetchRows
      conn
      (Query ("SELECT " <> fromQuery taskColumns <> " FROM tasks WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"))
      [MySQLInt64 (fromIntegral userId), MySQLInt64 (fromIntegral limit), MySQLInt64 (fromIntegral offset)]
  tasks <- attachLabelsToRows conn rows
  pure (tasks, total)

-- | v2(gRPC)向け: id昇順のkeyset(cursor)ページング。cursorの実体はuint64
-- (直前ページ最後のtask.id、0=先頭)、合成キーは使わない(CONTRACT.mdセクション5)
listCursor :: Pool MySQLConn -> Int -> Int -> Int -> IO [Task]
listCursor pool userId afterId limit = withConn pool $ \conn -> do
  rows <-
    if afterId > 0
      then
        fetchRows
          conn
          (Query ("SELECT " <> fromQuery taskColumns <> " FROM tasks WHERE user_id = ? AND id > ? ORDER BY id ASC LIMIT ?"))
          [MySQLInt64 (fromIntegral userId), MySQLInt64 (fromIntegral afterId), MySQLInt64 (fromIntegral limit)]
      else
        fetchRows
          conn
          (Query ("SELECT " <> fromQuery taskColumns <> " FROM tasks WHERE user_id = ? ORDER BY id ASC LIMIT ?"))
          [MySQLInt64 (fromIntegral userId), MySQLInt64 (fromIntegral limit)]
  attachLabelsToRows conn rows

-- | 他ユーザーのtaskは見えない(所有権分離)
findById :: Pool MySQLConn -> Int -> Int -> IO (Either TaskError Task)
findById pool taskId userId = withConn pool $ \conn -> do
  rows <-
    fetchRows
      conn
      (Query ("SELECT " <> fromQuery taskColumns <> " FROM tasks WHERE id = ? AND user_id = ?"))
      [MySQLInt64 (fromIntegral taskId), MySQLInt64 (fromIntegral userId)]
  case rows of
    [] -> pure (Left (TaskError NotFound "not_found"))
    _ -> do
      tasks <- attachLabelsToRows conn rows
      pure (case tasks of
              (t : _) -> Right t
              [] -> Left (TaskError NotFound "not_found"))

create :: Pool MySQLConn -> Int -> TaskInput -> Int -> IO Int
create pool userId input statusDb = withConn pool $ \conn -> withTransaction conn $ do
  now <- nowLocal
  _ <-
    execute
      conn
      "INSERT INTO tasks (name, description, status, finished_on, user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
      [ MySQLText (tiName input)
      , maybe MySQLNull MySQLText (tiDescription input)
      , MySQLInt32 (fromIntegral statusDb)
      , MySQLDate (tiFinishedOn input)
      , MySQLInt64 (fromIntegral userId)
      , MySQLDateTime now
      , MySQLDateTime now
      ]
  newId <- lastInsertIdOf conn
  insertLabels conn newId (tiLabelIds input) now
  pure newId

-- | 戻り値: True=更新できた, False=対象行が(他人のtaskも含め)見つからない。
-- 【実機検証で確認する必要がある実バグの回避、backend-python/backend-elixirと同じ観点】
-- MySQLのUPDATE文のaffected_rowsは、既定では「WHERE句にマッチした行数」ではなく
-- 「実際に値が変化した行数」を返す(mysql-haskellはClientFoundRowsフラグを立てる
-- オプションを公開していないため、この既定の挙動をそのまま受ける)。DATETIME列は秒精度
-- しか持たないため、同じ秒内に作成・更新すると(かつ他のSET対象列の値も偶然全て同じだと)
-- updated_atを含め1列も値が変わらず、WHERE句は行にマッチしているのにaffected_rows=0に
-- なりうる。ここではaffected_rowsに頼らず、UPDATE実行後に対象行の存在を独立に
-- SELECT COUNT(*)で確認することで、この既知の落とし穴を設計により回避している
-- (backend-pythonはCLIENT_FOUND_ROWSフラグで解決したが、mysql-haskellにはこのフラグを
-- 設定する手段が公開されていないため、Haskellでは「存在確認を独立したクエリに分離する」
-- という別の回避策を取っている)
update :: Pool MySQLConn -> Int -> Int -> TaskInput -> Int -> IO Bool
update pool taskId userId input statusDb = withConn pool $ \conn -> withTransaction conn $ do
  now <- nowLocal
  _ <-
    execute
      conn
      "UPDATE tasks SET name = ?, description = ?, status = ?, finished_on = ?, updated_at = ? WHERE id = ? AND user_id = ?"
      [ MySQLText (tiName input)
      , maybe MySQLNull MySQLText (tiDescription input)
      , MySQLInt32 (fromIntegral statusDb)
      , MySQLDate (tiFinishedOn input)
      , MySQLDateTime now
      , MySQLInt64 (fromIntegral taskId)
      , MySQLInt64 (fromIntegral userId)
      ]
  exists <- rowExists conn taskId userId
  if exists
    then insertLabels conn taskId (tiLabelIds input) now >> pure True
    else pure False

rowExists :: MySQLConn -> Int -> Int -> IO Bool
rowExists conn taskId userId = do
  rows <- fetchRows conn "SELECT COUNT(*) FROM tasks WHERE id = ? AND user_id = ?" [MySQLInt64 (fromIntegral taskId), MySQLInt64 (fromIntegral userId)]
  case rows of
    [[c]] -> pure (asInt c > 0)
    _ -> pure False

-- | tasksとtask_labelsの削除を1つのトランザクションで包む。task_labelsには外部キー制約が
-- 無い(migrations/000004)ため、トランザクション無しで個別にDELETEすると、両文の間で
-- プロセスが落ちた場合にtask_labelsの孤立行が残り得る(backend-rust/backend-c/backend-cpp/
-- backend-java/backend-kotlin/backend-python/backend-elixirと同じ設計。backend-rustには
-- かつてこの保護が欠けている既知バグがあり、後に修正された経緯がある)
delete :: Pool MySQLConn -> Int -> Int -> IO Bool
delete pool taskId userId = withConn pool $ \conn -> withTransaction conn $ do
  exists <- rowExists conn taskId userId
  if not exists
    then pure False
    else do
      _ <- execute conn "DELETE FROM task_labels WHERE task_id = ?" [MySQLInt64 (fromIntegral taskId)]
      _ <- execute conn "DELETE FROM tasks WHERE id = ? AND user_id = ?" [MySQLInt64 (fromIntegral taskId), MySQLInt64 (fromIntegral userId)]
      pure True

-- | label_idsに同じidが重複して含まれる場合(例: [3,3,5])、重複除去せずそのままINSERTすると
-- 2回目の(task_id,3)でtask_labelsの(task_id,label_id)へのUNIQUE制約(migrations/000004)に
-- 違反する(backend(Go)・backend-rust・backend-java・backend-kotlin・backend-python・
-- backend-elixirで見つかった同根のバグ)。nubは最初の出現順を保つ
insertLabels :: MySQLConn -> Int -> [Int] -> LocalTime -> IO ()
insertLabels conn taskId labelIds now = do
  _ <- execute conn "DELETE FROM task_labels WHERE task_id = ?" [MySQLInt64 (fromIntegral taskId)]
  let deduped = nub labelIds
  mapM_
    ( \lid ->
        execute
          conn
          "INSERT INTO task_labels (task_id, label_id, created_at, updated_at) VALUES (?, ?, ?, ?)"
          [MySQLInt64 (fromIntegral taskId), MySQLInt64 (fromIntegral lid), MySQLDateTime now, MySQLDateTime now]
    )
    deduped

lastInsertIdOf :: MySQLConn -> IO Int
lastInsertIdOf conn = do
  rows <- fetchRows conn "SELECT LAST_INSERT_ID()" []
  case rows of
    [[v]] -> pure (asInt v)
    _ -> error "TaskRepository: failed to read LAST_INSERT_ID()"

-- | JWT認証のuser_id解決用。ローカル発行issuerのsubはusers.idそのもの、存在確認のみ行う
findUserById :: Pool MySQLConn -> Int -> IO (Maybe User)
findUserById pool uid = withConn pool $ \conn -> do
  rows <- fetchRows conn "SELECT id, email, name FROM users WHERE id = ?" [MySQLInt64 (fromIntegral uid)]
  pure $ case rows of
    [[idV, emailV, nameV]] -> Just (User {userId = asInt idV, userEmail = asText emailV, userName = asText nameV})
    _ -> Nothing

-- | Keycloak発行issuerのsub=keycloak_subは、usersテーブルには無くuser_keycloaksテーブルに
-- 分離されている(migration 000008_split_user_credentials、CONTRACT.mdセクション16.2)ため
-- JOIN経由で引く
findUserByKeycloakSub :: Pool MySQLConn -> Text -> IO (Maybe User)
findUserByKeycloakSub pool keycloakSub = withConn pool $ \conn -> do
  rows <-
    fetchRows
      conn
      "SELECT u.id, u.email, u.name FROM users u JOIN user_keycloaks uk ON uk.user_id = u.id WHERE uk.keycloak_sub = ?"
      [MySQLText keycloakSub]
  pure $ case rows of
    [[idV, emailV, nameV]] -> Just (User {userId = asInt idV, userEmail = asText emailV, userName = asText nameV})
    _ -> Nothing
