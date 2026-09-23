-- | REST/gRPCの両トランスポートが共有するuser_id解決ロジック(認証ロジックを複製しない設計)。
-- backend(Go)のresolveUserID・backend-java/backend-kotlin/backend-python/backend-elixirの
-- UserResolverと同じ分岐:
--     - ローカル(HMAC/RSA)発行のJWT: subは内部user_idそのもの -> usersをidで検索
--     - Keycloak発行のJWT: subはkeycloak_sub -> user_keycloaks経由で検索
-- どちらも見つからなければuser_not_provisioned
module BackendHaskell.Auth.UserResolver
  ( resolveUserId
  ) where

import Data.Pool (Pool)
import Data.Text (Text)
import qualified Data.Text as T
import Text.Read (readMaybe)

import BackendHaskell.Auth.Claims (Claims (..))
import BackendHaskell.Auth.Dispatcher (Dispatcher, dispatchVerify, isLocalIssuer)
import BackendHaskell.Domain.Errors (TaskError (..), TaskErrorKind (..))
import BackendHaskell.Domain.Models (User (..))
import Database.MySQL.Base (MySQLConn)
import qualified BackendHaskell.Repository.TaskRepository as Repo

resolveUserId :: Pool MySQLConn -> Dispatcher -> Maybe Text -> IO (Either TaskError Int)
resolveUserId pool dispatcher maybeAuthHeader =
  case maybeAuthHeader >>= stripBearer of
    Nothing -> pure (Left unauthorized)
    Just token -> do
      verifyResult <- dispatchVerify dispatcher token
      case verifyResult of
        Left _ -> pure (Left unauthorized)
        Right claims -> resolveFromClaims pool claims

stripBearer :: Text -> Maybe Text
stripBearer header =
  let prefix = "Bearer "
   in if T.isPrefixOf prefix header && T.length header > T.length prefix
        then Just (T.drop (T.length prefix) header)
        else Nothing

resolveFromClaims :: Pool MySQLConn -> Claims -> IO (Either TaskError Int)
resolveFromClaims pool claims
  | isLocalIssuer (claimsIss claims) = case readMaybe (T.unpack (claimsSub claims)) :: Maybe Int of
      Nothing -> pure (Left userNotProvisioned)
      Just uid -> do
        found <- Repo.findUserById pool uid
        pure (maybe (Left userNotProvisioned) (Right . userId) found)
  | otherwise = do
      found <- Repo.findUserByKeycloakSub pool (claimsSub claims)
      pure (maybe (Left userNotProvisioned) (Right . userId) found)

unauthorized :: TaskError
unauthorized = TaskError Unauthorized "unauthorized"

userNotProvisioned :: TaskError
userNotProvisioned = TaskError UserNotProvisioned "user_not_provisioned"
