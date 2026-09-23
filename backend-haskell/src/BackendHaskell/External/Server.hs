-- | 外部公開API(/external/v1/tasks)のハンドラ。CONTRACT.mdセクション11:
-- Client Credentials Grantのみ受け付け(RequireExternalClientAuth相当、
-- BackendHaskell.Auth.ExternalAuth参照)、backend.external-tasks-pagination-v2で
-- offset(v1)/cursor(v2)を切り替える。既存のTaskRepository.listOffset/listCursorを
-- そのまま再利用し(新規のリポジトリ関数は書かない)、レスポンスのTask JSON形状も
-- 内部REST v1のTaskJson.taskToJsonをそのまま再利用する(user_idを含めない形状は
-- 外部公開APIにもそのまま当てはまる)
module BackendHaskell.External.Server
  ( externalServer
  ) where

import Control.Monad.IO.Class (liftIO)
import Data.Aeson (Value (..), object, (.=))
import qualified Data.Aeson as Aeson
import Data.Pool (Pool)
import Data.Text (Text)
import qualified Data.Text as T
import Servant

import BackendHaskell.Auth.Dispatcher (Dispatcher)
import BackendHaskell.Auth.ExternalAuth (requireExternalClient)
import BackendHaskell.Domain.Errors (TaskError (..))
import BackendHaskell.Domain.Models (Task (..))
import BackendHaskell.External.Api (ExternalTaskAPI)
import BackendHaskell.External.Query
  ( cursorAfterId
  , cursorLimit
  , offsetPage
  , offsetPageSize
  , parseUserId
  )
import BackendHaskell.Flags.FeatureFlagCache (FeatureFlagCache, variation)
import BackendHaskell.Logging (logDebug)
import BackendHaskell.Repository.TaskRepository (listCursor, listOffset)
import BackendHaskell.Rest.ErrorMapper (statusAndBody)
import BackendHaskell.Rest.TaskJson (taskToJson)
import Database.MySQL.Base (MySQLConn)

externalServer :: Pool MySQLConn -> Dispatcher -> Text -> FeatureFlagCache -> Server ExternalTaskAPI
externalServer pool dispatcher expectedClientId flagCache authHeader userIdParam pageParam pageSizeParam cursorParam limitParam = do
  authorizeOrThrow dispatcher expectedClientId authHeader
  userId <- either raiseTaskError pure (parseUserId userIdParam)
  useV2 <- liftIO ((== "on") <$> variation flagCache "backend.external-tasks-pagination-v2" "off")
  liftIO (logDebug "external" ("user_id=" <> T.pack (show userId) <> " pagination_v2=" <> T.pack (show useV2)))
  if useV2
    then cursorResponse pool userId cursorParam limitParam
    else offsetResponse pool userId pageParam pageSizeParam

authorizeOrThrow :: Dispatcher -> Text -> Maybe Text -> Handler ()
authorizeOrThrow dispatcher expectedClientId authHeader = do
  result <- liftIO (requireExternalClient dispatcher expectedClientId authHeader)
  either raiseTaskError pure result

offsetResponse :: Pool MySQLConn -> Int -> Maybe Int -> Maybe Int -> Handler Value
offsetResponse pool userId pageParam pageSizeParam = do
  let page = offsetPage pageParam
      pageSize = offsetPageSize pageSizeParam
      offset = (page - 1) * pageSize
  (tasks, total) <- liftIO (listOffset pool userId pageSize offset)
  pure (object ["tasks" .= map taskToJson tasks, "page" .= page, "page_size" .= pageSize, "total" .= total])

cursorResponse :: Pool MySQLConn -> Int -> Maybe Text -> Maybe Int -> Handler Value
cursorResponse pool userId cursorParam limitParam = do
  let afterId = cursorAfterId cursorParam
      limit = cursorLimit limitParam
  tasks <- liftIO (listCursor pool userId afterId limit)
  let nextCursor = case tasks of
        [] -> Null
        _ -> String (T.pack (show (taskId (last tasks))))
  pure (object ["tasks" .= map taskToJson tasks, "next_cursor" .= nextCursor, "limit" .= limit])

raiseTaskError :: TaskError -> Handler a
raiseTaskError err = throwError (taskErrorToServerError err)

taskErrorToServerError :: TaskError -> ServerError
taskErrorToServerError err =
  let (status, body) = statusAndBody err
   in ServerError
        { errHTTPCode = status
        , errReasonPhrase = "Error"
        , errBody = Aeson.encode body
        , errHeaders = [("Content-Type", "application/json")]
        }
