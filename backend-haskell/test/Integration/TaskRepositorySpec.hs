-- | 実DB(docker-compose上のMySQL)結合テスト。テストごとに一意なuser/labelを作って検証する
-- (backend-java/backend-kotlin/backend-python/backend-elixirのTaskRepositoryTestと同じ
-- シナリオ構成)
module TaskRepositorySpec (spec) where

import Control.Exception (finally)
import Data.List (sort)
import qualified Data.Text as T
import Data.Time.Calendar (fromGregorian)
import Test.Hspec

import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Domain.Models (Label (..), Task (..), TaskInput (..))
import qualified BackendHaskell.Repository.TaskRepository as Repo
import DbFixture

baseInput :: TaskInput
baseInput =
  TaskInput
    { tiName = "task"
    , tiDescription = Just "desc"
    , tiStatusRaw = "waiting"
    , tiFinishedOn = fromGregorian 2099 1 1
    , tiLabelIds = []
    }

spec :: Spec
spec = around withUser $ describe "TaskRepository" $ do
  it "create find update delete round trip" $ \userId -> do
    taskId <- Repo.create pool userId baseInput 1
    Right task <- Repo.findById pool taskId userId
    taskName task `shouldBe` "task"
    taskStatusWire task `shouldBe` "waiting"

    ok <- Repo.update pool taskId userId (baseInput {tiName = "updated", tiStatusRaw = "completed"}) 3
    ok `shouldBe` True
    Right updated <- Repo.findById pool taskId userId
    taskName updated `shouldBe` "updated"
    taskStatusWire updated `shouldBe` "completed"

    deleted <- Repo.delete pool taskId userId
    deleted `shouldBe` True
    afterDelete <- Repo.findById pool taskId userId
    case afterDelete of
      Left err -> teKind err `shouldBe` NotFound
      Right _ -> expectationFailure "expected NotFound after delete"

  it "update of nonexistent task returns false" $ \userId -> do
    ok <- Repo.update pool 999999999 userId baseInput 1
    ok `shouldBe` False

  it "delete of nonexistent task returns false" $ \userId -> do
    ok <- Repo.delete pool 999999999 userId
    ok `shouldBe` False

  it "delete removes task_labels rows" $ \userId -> do
    suffix <- uniqueSuffix
    labelId <- createLabel suffix
    ( do
        taskId <- Repo.create pool userId (baseInput {tiLabelIds = [labelId]}) 1
        countBefore <- countTaskLabels taskId
        countBefore `shouldBe` 1

        _ <- Repo.delete pool taskId userId
        countAfter <- countTaskLabels taskId
        countAfter `shouldBe` 0
      )
      `finally` cleanupLabel labelId

  it "create dedups duplicate label ids" $ \userId -> do
    suffix <- uniqueSuffix
    labelA <- createLabel (suffix <> "-a")
    labelB <- createLabel (suffix <> "-b")
    ( do
        taskId <- Repo.create pool userId (baseInput {tiLabelIds = [labelA, labelA, labelB]}) 1
        Right task <- Repo.findById pool taskId userId
        sort (map labelId (taskLabels task)) `shouldBe` sort [labelA, labelB]
      )
      `finally` (cleanupLabel labelA >> cleanupLabel labelB)

  it "update dedups duplicate label ids" $ \userId -> do
    suffix <- uniqueSuffix
    labelA <- createLabel (suffix <> "-a")
    labelB <- createLabel (suffix <> "-b")
    ( do
        taskId <- Repo.create pool userId baseInput 1
        ok <- Repo.update pool taskId userId (baseInput {tiLabelIds = [labelB, labelB, labelA]}) 1
        ok `shouldBe` True
        Right task <- Repo.findById pool taskId userId
        sort (map labelId (taskLabels task)) `shouldBe` sort [labelA, labelB]
      )
      `finally` (cleanupLabel labelA >> cleanupLabel labelB)

  it "other user cannot see task" $ \userId -> do
    suffix <- uniqueSuffix
    otherUserId <- createUser (suffix <> "-other")
    ( do
        taskId <- Repo.create pool userId baseInput 1
        result <- Repo.findById pool taskId otherUserId
        case result of
          Left err -> teKind err `shouldBe` NotFound
          Right _ -> expectationFailure "expected NotFound for other user"
      )
      `finally` cleanupUser otherUserId

  it "list_offset returns total and respects limit/offset" $ \userId -> do
    _ <- mapM (\i -> Repo.create pool userId (baseInput {tiName = "task-" <> T.pack (show (i :: Int))}) 1) [1, 2, 3]
    (page1, total1) <- Repo.listOffset pool userId 2 0
    total1 `shouldBe` 3
    length page1 `shouldBe` 2
    (page2, total2) <- Repo.listOffset pool userId 2 2
    total2 `shouldBe` 3
    length page2 `shouldBe` 1

  it "list_cursor orders by id ascending and respects after_id" $ \userId -> do
    id1 <- Repo.create pool userId (baseInput {tiName = "first"}) 1
    id2 <- Repo.create pool userId (baseInput {tiName = "second"}) 1
    id3 <- Repo.create pool userId (baseInput {tiName = "third"}) 1
    page1 <- Repo.listCursor pool userId 0 2
    map taskId page1 `shouldBe` [id1, id2]
    page2 <- Repo.listCursor pool userId id2 2
    map taskId page2 `shouldBe` [id3]

  it "find_user_by_id returns user when exists" $ \userId -> do
    result <- Repo.findUserById pool userId
    fmap (\u -> u) result `shouldSatisfy` maybe False (const True)

  it "find_user_by_id returns Nothing when not found" $ \_userId -> do
    result <- Repo.findUserById pool 999999999
    result `shouldSatisfy` maybe True (const False)

  it "find_user_by_keycloak_sub returns user when exists" $ \_userId -> do
    suffix <- uniqueSuffix
    let keycloakSub = "keycloak-sub-" <> suffix
    kcUserId <- createUserWithKeycloakSub (suffix <> "-kc") keycloakSub
    ( do
        result <- Repo.findUserByKeycloakSub pool keycloakSub
        result `shouldSatisfy` maybe False (const True)
      )
      `finally` cleanupUser kcUserId

  it "find_user_by_keycloak_sub returns Nothing when not found" $ \_userId -> do
    result <- Repo.findUserByKeycloakSub pool "nonexistent-sub"
    result `shouldSatisfy` maybe True (const False)

-- | テストが例外(パターン不一致の`shouldBe`失敗等)で中断しても後始末が必ず走るよう
-- `finally`で保護する
withUser :: (Int -> IO a) -> IO a
withUser action = do
  suffix <- uniqueSuffix
  userId <- createUser suffix
  action userId `finally` cleanupUser userId
