-- | 実際にgrapesyサーバー(実DBに接続)をテスト用ポートで起動し、grapesyクライアントの
-- 生API(withRPC/sendFinalInput/recvFinalOutput)経由でgRPC v2の認証・CRUD・cursorページングを
-- 検証する結合テスト(backend-java/backend-kotlin/backend-python/backend-elixirの
-- GrpcServiceTestと同じシナリオ構成)
module GrpcServiceSpec (spec) where

import Control.Concurrent (forkIO, threadDelay)
import Control.Exception (SomeException, finally, try)
import Data.Proxy (Proxy (..))
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import System.IO.Unsafe (unsafePerformIO)
import Test.Hspec

import Network.GRPC.Client
import Network.GRPC.Common
import Network.GRPC.Common.Protobuf
import Network.GRPC.Server.Run
import Network.GRPC.Server.StreamType (fromMethods)
import Network.GRPC.Spec (CustomMetadata (CustomMetadata))

import BackendHaskell.Auth.Dispatcher
import BackendHaskell.Auth.HmacVerifier (newHmacVerifier)
import BackendHaskell.Grpc.TaskService (taskServiceMethods)
import Proto.API.Task.V1.Task

import DbFixture (cleanupUser, countTaskLabels, createUser, pool, uniqueSuffix)
import TestTokenHelper (makeHmacToken)

testHmacSecret :: Text
testHmacSecret = "grpc-test-secret-that-is-at-least-32-bytes-long"

testGrpcPort :: Int
testGrpcPort = 19107

-- | テストプロセス全体で1つだけ実サーバーを立てる(テストごとに起動・停止すると遅く、
-- ポートの再利用待ちで不安定になりうるため)。DbFixture.poolと同じ遅延シングルトンパターン
{-# NOINLINE serverStarted #-}
serverStarted :: ()
serverStarted = unsafePerformIO $ do
  let dispatcher =
        registerVerifier localHmacIssuer (newHmacVerifier testHmacSecret localHmacIssuer "backend") newDispatcher
  _ <-
    forkIO $
      runServerWithHandlers
        def
        ServerConfig
          { serverInsecure = Just (InsecureConfig (Just "127.0.0.1") (fromIntegral testGrpcPort))
          , serverSecure = Nothing
          }
        (fromMethods (taskServiceMethods pool dispatcher))
  threadDelay 300000
  pure ()

authTokenFor :: Int -> IO Text
authTokenFor userId = do
  token <- makeHmacToken testHmacSecret localHmacIssuer "backend" (T.pack (show userId)) 3600
  pure ("Bearer " <> token)

authedParams :: Text -> CallParams (Protobuf TaskService meth)
authedParams bearer = def {callRequestMetadata = [CustomMetadata "authorization" (TE.encodeUtf8 bearer)]}

withConn :: (Connection -> IO a) -> IO a
withConn action = do
  serverStarted `seq` pure ()
  withConnection
    def
    ( ServerInsecure
        Address
          { addressHost = "127.0.0.1"
          , addressPort = fromIntegral testGrpcPort
          , addressAuthority = Nothing
          }
    )
    action

createTask :: Connection -> Text -> Proto CreateTaskRequest -> IO (Proto Task)
createTask conn bearer req =
  withRPC conn (authedParams bearer) (Proxy @(Protobuf TaskService "createTask")) $ \call -> do
    sendFinalInput call req
    (out, _) <- recvFinalOutput call
    pure out

getTask :: Connection -> Text -> Proto GetTaskRequest -> IO (Proto Task)
getTask conn bearer req =
  withRPC conn (authedParams bearer) (Proxy @(Protobuf TaskService "getTask")) $ \call -> do
    sendFinalInput call req
    (out, _) <- recvFinalOutput call
    pure out

updateTask :: Connection -> Text -> Proto UpdateTaskRequest -> IO (Proto Task)
updateTask conn bearer req =
  withRPC conn (authedParams bearer) (Proxy @(Protobuf TaskService "updateTask")) $ \call -> do
    sendFinalInput call req
    (out, _) <- recvFinalOutput call
    pure out

deleteTask :: Connection -> Text -> Proto DeleteTaskRequest -> IO (Proto DeleteTaskResponse)
deleteTask conn bearer req =
  withRPC conn (authedParams bearer) (Proxy @(Protobuf TaskService "deleteTask")) $ \call -> do
    sendFinalInput call req
    (out, _) <- recvFinalOutput call
    pure out

listTasks :: Connection -> Text -> Proto ListTasksRequest -> IO (Proto ListTasksResponse)
listTasks conn bearer req =
  withRPC conn (authedParams bearer) (Proxy @(Protobuf TaskService "listTasks")) $ \call -> do
    sendFinalInput call req
    (out, _) <- recvFinalOutput call
    pure out

mkCreateReq :: Text -> Proto CreateTaskRequest
mkCreateReq name =
  Proto
    ( defMessage
        & #name .~ name
        & #status .~ ("waiting" :: Text)
        & #finishedOn .~ ("2099-01-01" :: Text)
        & #labelIds .~ []
    )

spec :: Spec
spec = around withUser $ describe "GrpcService" $ do
  it "full CRUD round trip over real gRPC connection" $ \(userId, bearer) -> withConn $ \conn -> do
    created <- getProto <$> createTask conn bearer (mkCreateReq "grpc-task")
    created ^. #name `shouldBe` "grpc-task"
    created ^. #status `shouldBe` "waiting"
    let taskIdVal = created ^. #id

    fetched <- getProto <$> getTask conn bearer (Proto (defMessage & #id .~ taskIdVal))
    fetched ^. #name `shouldBe` "grpc-task"

    updated <-
      getProto
        <$> updateTask
          conn
          bearer
          ( Proto
              ( defMessage
                  & #id .~ taskIdVal
                  & #name .~ ("grpc-task-updated" :: Text)
                  & #status .~ ("completed" :: Text)
                  & #finishedOn .~ ("2099-02-02" :: Text)
                  & #labelIds .~ []
              )
          )
    updated ^. #name `shouldBe` "grpc-task-updated"
    updated ^. #status `shouldBe` "completed"

    _ <- deleteTask conn bearer (Proto (defMessage & #id .~ taskIdVal))
    result <- try (getTask conn bearer (Proto (defMessage & #id .~ taskIdVal))) :: IO (Either SomeException (Proto Task))
    case result of
      Left _ -> pure ()
      Right _ -> expectationFailure "expected GetTask to fail with NotFound after delete"

  it "delete removes task_labels rows" $ \(_userId, bearer) -> withConn $ \conn -> do
    created <- getProto <$> createTask conn bearer (mkCreateReq "grpc-task-labels")
    let taskIdVal = created ^. #id
    _ <- deleteTask conn bearer (Proto (defMessage & #id .~ taskIdVal))
    countAfter <- countTaskLabels (fromIntegral taskIdVal)
    countAfter `shouldBe` 0

  it "unauthenticated call is rejected" $ \(_userId, _bearer) -> withConn $ \conn -> do
    result <-
      try (createTask conn "Bearer not-a-valid-token" (mkCreateReq "grpc-task-unauth"))
        :: IO (Either SomeException (Proto Task))
    case result of
      Left _ -> pure ()
      Right _ -> expectationFailure "expected unauthenticated call to be rejected"

  it "expired token is rejected" $ \(userId, _bearer) -> withConn $ \conn -> do
    expiredToken <- makeHmacToken testHmacSecret localHmacIssuer "backend" (T.pack (show userId)) (-3600)
    result <-
      try (createTask conn ("Bearer " <> expiredToken) (mkCreateReq "grpc-task-expired"))
        :: IO (Either SomeException (Proto Task))
    case result of
      Left _ -> pure ()
      Right _ -> expectationFailure "expected expired token to be rejected"

  it "cursor pagination chains to no next page" $ \(_userId, bearer) -> withConn $ \conn -> do
    _ <- createTask conn bearer (mkCreateReq "grpc-cursor-1")
    _ <- createTask conn bearer (mkCreateReq "grpc-cursor-2")
    _ <- createTask conn bearer (mkCreateReq "grpc-cursor-3")

    page1 <- getProto <$> listTasks conn bearer (Proto (defMessage & #cursor .~ 0 & #limit .~ 2))
    length (page1 ^. #tasks) `shouldBe` 2
    let cursor1 = page1 ^. #nextCursor
    cursor1 `shouldSatisfy` (/= 0)

    page2 <- getProto <$> listTasks conn bearer (Proto (defMessage & #cursor .~ cursor1 & #limit .~ 2))
    length (page2 ^. #tasks) `shouldBe` 1
    page2 ^. #nextCursor `shouldBe` 0

-- | テストが例外で失敗しても(例: gRPC呼び出しがGrpcException/HTTP2Errorを投げても)
-- 後始末が必ず走るよう`finally`で保護する(素朴な逐次実行では、途中で例外が飛んだ場合に
-- cleanupUserがスキップされ、テストDBに行が残ってしまう)
withUser :: ((Int, Text) -> IO a) -> IO a
withUser action = do
  suffix <- uniqueSuffix
  userId <- createUser suffix
  bearer <- authTokenFor userId
  action (userId, bearer) `finally` cleanupUser userId
