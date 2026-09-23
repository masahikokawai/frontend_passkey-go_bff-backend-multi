-- | 内部REST v1(:8119)。Servantが`Api.hs`で宣言した型からルーティング・リクエスト解析・
-- レスポンスシリアライズを導出する(README.md「アーキテクチャ選定」節参照)
module BackendHaskell.Rest.Server
  ( taskServer
  ) where

import Control.Monad.IO.Class (liftIO)
import Data.Aeson (Value (..), object, (.:), (.:?), (.=))
import qualified Data.Aeson as Aeson
import qualified Data.Aeson.KeyMap as KM
import Data.Aeson.Types (parseMaybe)
import Data.Pool (Pool)
import Data.Text (Text)
import qualified Data.Text as T
import Data.Time.Clock (getCurrentTime, utctDay)
import Servant

import BackendHaskell.Auth.Dispatcher (Dispatcher)
import BackendHaskell.Auth.UserResolver (resolveUserId)
import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Logging (logDebug)
import BackendHaskell.Domain.Models (TaskInput (..))
import BackendHaskell.Domain.Validation (parseFinishedOn, validateTaskInput)
import BackendHaskell.Repository.TaskRepository
import qualified BackendHaskell.Repository.TaskRepository as Repo
import BackendHaskell.Rest.Api (TaskAPI)
import BackendHaskell.Rest.ErrorMapper (statusAndBody)
import BackendHaskell.Rest.TaskJson (taskToJson)
import Database.MySQL.Base (MySQLConn)

taskServer :: Pool MySQLConn -> Dispatcher -> Server TaskAPI
taskServer pool dispatcher =
  listHandler pool dispatcher
    :<|> createHandler pool dispatcher
    :<|> getHandler pool dispatcher
    :<|> updateHandler pool dispatcher
    :<|> deleteHandler pool dispatcher

authenticate :: Pool MySQLConn -> Dispatcher -> Maybe Text -> Handler Int
authenticate pool dispatcher authHeader = do
  result <- liftIO (resolveUserId pool dispatcher authHeader)
  case result of
    Right uid -> liftIO (logDebug "auth" ("resolved user_id=" <> T.pack (show uid)))
    Left _ -> pure ()
  either raiseTaskError pure result

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

parseIdText :: Text -> Either TaskError Int
parseIdText raw = case reads (T.unpack raw) of
  [(n, "")] -> Right n
  _ -> Left (TaskError InvalidId "invalid_id")

listHandler :: Pool MySQLConn -> Dispatcher -> Maybe Text -> Maybe Int -> Maybe Int -> Handler Value
listHandler pool dispatcher authHeader maybeLimit maybeOffset = do
  userId <- authenticate pool dispatcher authHeader
  let limit = maybe 20 id maybeLimit
      offset = maybe 0 id maybeOffset
  liftIO (logDebug "rest" ("list query limit=" <> T.pack (show limit) <> " offset=" <> T.pack (show offset)))
  (tasks, total) <- liftIO (Repo.listOffset pool userId limit offset)
  pure (object ["tasks" .= map taskToJson tasks, "total" .= total, "limit" .= limit, "offset" .= offset])

createHandler :: Pool MySQLConn -> Dispatcher -> Maybe Text -> Value -> Handler Value
createHandler pool dispatcher authHeader body = do
  userId <- authenticate pool dispatcher authHeader
  input <- either raiseTaskError pure (parseTaskInput body)
  today <- liftIO (utctDay <$> getCurrentTime)
  (statusDb, _) <- either raiseTaskError pure (validateTaskInput input today)
  newId <- liftIO (Repo.create pool userId input statusDb)
  found <- liftIO (Repo.findById pool newId userId)
  either raiseTaskError (pure . taskToJson) found

getHandler :: Pool MySQLConn -> Dispatcher -> Text -> Maybe Text -> Handler Value
getHandler pool dispatcher idRaw authHeader = do
  userId <- authenticate pool dispatcher authHeader
  taskId <- either raiseTaskError pure (parseIdText idRaw)
  found <- liftIO (Repo.findById pool taskId userId)
  either raiseTaskError (pure . taskToJson) found

updateHandler :: Pool MySQLConn -> Dispatcher -> Text -> Maybe Text -> Value -> Handler Value
updateHandler pool dispatcher idRaw authHeader body = do
  userId <- authenticate pool dispatcher authHeader
  taskId <- either raiseTaskError pure (parseIdText idRaw)
  input <- either raiseTaskError pure (parseTaskInput body)
  today <- liftIO (utctDay <$> getCurrentTime)
  (statusDb, _) <- either raiseTaskError pure (validateTaskInput input today)
  ok <- liftIO (Repo.update pool taskId userId input statusDb)
  if not ok
    then raiseTaskError (TaskError NotFound "not_found")
    else do
      found <- liftIO (Repo.findById pool taskId userId)
      either raiseTaskError (pure . taskToJson) found

deleteHandler :: Pool MySQLConn -> Dispatcher -> Text -> Maybe Text -> Handler NoContent
deleteHandler pool dispatcher idRaw authHeader = do
  userId <- authenticate pool dispatcher authHeader
  taskId <- either raiseTaskError pure (parseIdText idRaw)
  ok <- liftIO (Repo.delete pool taskId userId)
  if not ok then raiseTaskError (TaskError NotFound "not_found") else pure NoContent

-- | backend(Go)のtaskRequestBody(binding:"required")と同じ「空文字も必須違反」の扱い
parseTaskInput :: Value -> Either TaskError TaskInput
parseTaskInput (Object o) =
  case parseMaybe parser o of
    Nothing -> Left (TaskError InvalidRequest "invalid_request")
    Just (nameRaw, descRaw, statusRaw, finishedOnRaw, labelIdsRaw) ->
      if blank nameRaw || blank statusRaw || blank finishedOnRaw
        then Left (TaskError InvalidRequest "invalid_request")
        else do
          finishedOn <- parseFinishedOn finishedOnRaw
          Right
            TaskInput
              { tiName = nameRaw
              , tiDescription = descRaw
              , tiStatusRaw = statusRaw
              , tiFinishedOn = finishedOn
              , tiLabelIds = maybe [] id labelIdsRaw
              }
  where
    parser obj = do
      n <- obj .:? "name" Aeson..!= ""
      d <- obj .:? "description"
      s <- obj .:? "status" Aeson..!= ""
      f <- obj .:? "finished_on" Aeson..!= ""
      l <- obj .:? "label_ids"
      pure (n, d, s, f, l)
    blank = T.null
parseTaskInput _ = Left (TaskError InvalidRequest "invalid_request")
