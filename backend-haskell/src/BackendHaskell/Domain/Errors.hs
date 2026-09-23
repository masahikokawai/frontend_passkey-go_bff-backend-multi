-- | backend-java/backend-kotlin/backend-python/backend-elixir/backend-rustのTaskErrorと
-- 同じ分類。JSON形状・HTTPステータス・gRPCステータスへの変換はトランスポート層(Rest/Grpc)が
-- それぞれ担う。
module BackendHaskell.Domain.Errors
  ( TaskError (..)
  , TaskErrorKind (..)
  ) where

import Data.Text (Text)

data TaskErrorKind
  = Unauthorized
  | UserNotProvisioned
  | InvalidRequest
  | InvalidId
  | InvalidStatus
  | InvalidFinishedOn
  | Validation
  | NotFound
  | Internal
  deriving (Eq, Show)

data TaskError = TaskError
  { teKind :: TaskErrorKind
  , teMessage :: Text
  }
  deriving (Eq, Show)
