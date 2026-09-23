-- | 内部gRPC v2(:9105)。REST v1と同じRepository・ドメインモデル・認証(Dispatcher/UserResolver)を
-- 共有する(認証・バリデーション・トランザクション保護のロジックを複製しない設計、
-- backend-java/backend-kotlin/backend-python/backend-elixirのgrpcサービスと同じ方針)。
--
-- grapesyの`mkNonStreaming`(単純な`Input -> IO Output`関数)ではgRPCメタデータ(authorization
-- ヘッダ)へアクセスできないため、より低レベルな`RawMethod`+`mkRpcHandlerNoDefMetadata`
-- (`Call rpc -> IO ()`という、Callの生の値を受け取るスタイル)を使う。これは
-- grapesyのtutorials/metadata/app/Server.hsと同じ低レベルAPIの使い方である。
module BackendHaskell.Grpc.TaskService
  ( taskServiceMethods
  ) where

import Control.Exception (throwIO, try)
import Data.Pool (Pool)
import Data.Text (Text)
import qualified Data.Text.Encoding as TE
import Data.Time (Day)
import qualified Data.Time.Clock as Clock

import Network.GRPC.Common
import Network.GRPC.Common.Protobuf
import Network.GRPC.Server
import Network.GRPC.Server.Protobuf
import Network.GRPC.Server.StreamType (Methods (..))
import Network.GRPC.Spec (CustomMetadata (CustomMetadata), customMetadataMapToList, getHeaderName, requestMetadata)

import BackendHaskell.Auth.Dispatcher (Dispatcher)
import BackendHaskell.Auth.UserResolver (resolveUserId)
import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Domain.Models (taskId)
import BackendHaskell.Domain.Validation (validateTaskInput)
import qualified BackendHaskell.Grpc.ProtoMapper as PM
import qualified BackendHaskell.Repository.TaskRepository as Repo
import Database.MySQL.Base (MySQLConn)
import Proto.API.Task.V1.Task

-- 【型レベルの並び順に関する注意】`ProtobufMethodsOf TaskService`は.protoの宣言順ではなく
-- アルファベット順(createTask, deleteTask, getTask, listTasks, updateTask)の型レベルリストに
-- なる(proto-lens-protocが生成する`ServiceMethods`インスタンス、generated/Proto/Task/V1/Task.hs
-- 参照)。`RawMethod`の並びもこれに合わせる必要がある
taskServiceMethods :: Pool MySQLConn -> Dispatcher -> Methods IO (ProtobufMethodsOf TaskService)
taskServiceMethods pool dispatcher =
  RawMethod (mkRpcHandlerNoDefMetadata (handleCreateTask pool dispatcher))
    $ RawMethod (mkRpcHandlerNoDefMetadata (handleDeleteTask pool dispatcher))
    $ RawMethod (mkRpcHandlerNoDefMetadata (handleGetTask pool dispatcher))
    $ RawMethod (mkRpcHandlerNoDefMetadata (handleListTasks pool dispatcher))
    $ RawMethod (mkRpcHandlerNoDefMetadata (handleUpdateTask pool dispatcher))
    $ NoMoreMethods

authenticate :: Pool MySQLConn -> Dispatcher -> Call rpc -> IO Int
authenticate pool dispatcher call = do
  headers <- getRequestHeaders call
  let authHeader = lookupCustomMetadata "authorization" (customMetadataMapToList (requestMetadata headers))
  result <- resolveUserId pool dispatcher authHeader
  case result of
    Left err -> throwGrpc err
    Right uid -> pure uid

lookupCustomMetadata :: Text -> [CustomMetadata] -> Maybe Text
lookupCustomMetadata key = go
  where
    go [] = Nothing
    go (CustomMetadata name v : rest)
      | TE.decodeUtf8 (getHeaderName name) == key = Just (TE.decodeUtf8 v)
      | otherwise = go rest

throwGrpc :: TaskError -> IO a
throwGrpc (TaskError kind message) =
  throwIO
    GrpcException
      { grpcError = kindToGrpcError kind
      , grpcErrorMessage = Just message
      , grpcErrorDetails = Nothing
      , grpcErrorMetadata = []
      }

kindToGrpcError :: TaskErrorKind -> GrpcError
kindToGrpcError Unauthorized = GrpcUnauthenticated
kindToGrpcError UserNotProvisioned = GrpcPermissionDenied
kindToGrpcError NotFound = GrpcNotFound
kindToGrpcError InvalidRequest = GrpcInvalidArgument
kindToGrpcError InvalidId = GrpcInvalidArgument
kindToGrpcError InvalidStatus = GrpcInvalidArgument
kindToGrpcError InvalidFinishedOn = GrpcInvalidArgument
kindToGrpcError Validation = GrpcInvalidArgument
kindToGrpcError Internal = GrpcInternal

logged :: forall a. String -> IO a -> IO a
logged method action = do
  start <- Clock.getCurrentTime
  result <- try action :: IO (Either GrpcException a)
  end <- Clock.getCurrentTime
  let durationMs = round (realToFrac (Clock.diffUTCTime end start) * 1000 :: Double) :: Int
  case result of
    Right v -> do
      putStrLn ("grpc method=" <> method <> " status=OK duration_ms=" <> show durationMs)
      pure v
    Left exc -> do
      putStrLn ("grpc method=" <> method <> " status=" <> show (grpcError exc) <> " duration_ms=" <> show durationMs)
      throwIO exc

handleListTasks :: Pool MySQLConn -> Dispatcher -> Call (Protobuf TaskService "listTasks") -> IO ()
handleListTasks pool dispatcher call = logged "list_tasks" $ do
  setResponseInitialMetadata call NoMetadata
  userId <- authenticate pool dispatcher call
  req <- getProto <$> (recvFinalInput call :: IO (Proto ListTasksRequest))
  let lim = if req ^. #limit > 0 then fromIntegral (req ^. #limit) else 20
  tasks <- Repo.listCursor pool userId (fromIntegral (req ^. #cursor)) lim
  let nextCursor = if null tasks || length tasks < lim then 0 else fromIntegral (taskId (last tasks))
      resp = (defMessage :: ListTasksResponse) & #tasks .~ map PM.toProto tasks & #nextCursor .~ nextCursor
  sendFinalOutput call (Proto resp, NoMetadata)

handleGetTask :: Pool MySQLConn -> Dispatcher -> Call (Protobuf TaskService "getTask") -> IO ()
handleGetTask pool dispatcher call = logged "get_task" $ do
  setResponseInitialMetadata call NoMetadata
  userId <- authenticate pool dispatcher call
  req <- getProto <$> (recvFinalInput call :: IO (Proto GetTaskRequest))
  result <- Repo.findById pool (fromIntegral (req ^. #id)) userId
  case result of
    Left err -> throwGrpc err
    Right task -> sendFinalOutput call (Proto (PM.toProto task), NoMetadata)

handleCreateTask :: Pool MySQLConn -> Dispatcher -> Call (Protobuf TaskService "createTask") -> IO ()
handleCreateTask pool dispatcher call = logged "create_task" $ do
  setResponseInitialMetadata call NoMetadata
  userId <- authenticate pool dispatcher call
  req <- getProto <$> (recvFinalInput call :: IO (Proto CreateTaskRequest))
  case PM.fromCreateRequest req of
    Left err -> throwGrpc err
    Right input -> do
      today <- currentUtcDay
      case validateTaskInput input today of
        Left err -> throwGrpc err
        Right (statusDb, _) -> do
          newId <- Repo.create pool userId input statusDb
          found <- Repo.findById pool newId userId
          case found of
            Left err -> throwGrpc err
            Right task -> sendFinalOutput call (Proto (PM.toProto task), NoMetadata)

handleUpdateTask :: Pool MySQLConn -> Dispatcher -> Call (Protobuf TaskService "updateTask") -> IO ()
handleUpdateTask pool dispatcher call = logged "update_task" $ do
  setResponseInitialMetadata call NoMetadata
  userId <- authenticate pool dispatcher call
  req <- getProto <$> (recvFinalInput call :: IO (Proto UpdateTaskRequest))
  case PM.fromUpdateRequest req of
    Left err -> throwGrpc err
    Right input -> do
      today <- currentUtcDay
      case validateTaskInput input today of
        Left err -> throwGrpc err
        Right (statusDb, _) -> do
          ok <- Repo.update pool (fromIntegral (req ^. #id)) userId input statusDb
          if not ok
            then throwGrpc (TaskError NotFound "not_found")
            else do
              found <- Repo.findById pool (fromIntegral (req ^. #id)) userId
              case found of
                Left err -> throwGrpc err
                Right task -> sendFinalOutput call (Proto (PM.toProto task), NoMetadata)

handleDeleteTask :: Pool MySQLConn -> Dispatcher -> Call (Protobuf TaskService "deleteTask") -> IO ()
handleDeleteTask pool dispatcher call = logged "delete_task" $ do
  setResponseInitialMetadata call NoMetadata
  userId <- authenticate pool dispatcher call
  req <- getProto <$> (recvFinalInput call :: IO (Proto DeleteTaskRequest))
  ok <- Repo.delete pool (fromIntegral (req ^. #id)) userId
  if not ok
    then throwGrpc (TaskError NotFound "not_found")
    else sendFinalOutput call (Proto (defMessage :: DeleteTaskResponse), NoMetadata)

currentUtcDay :: IO Day
currentUtcDay = Clock.utctDay <$> Clock.getCurrentTime
