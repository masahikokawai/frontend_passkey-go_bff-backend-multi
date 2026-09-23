-- | 外部公開API(:8120予定)の型定義。内部REST v1(Rest.Api)と同じ「APIの形を型として書く」
-- Servantの設計を、2つ目の(より小さい)エンドポイントで再度適用する
module BackendHaskell.External.Api
  ( ExternalTaskAPI
  ) where

import Data.Aeson (Value)
import Data.Text (Text)
import Servant

type ExternalTaskAPI =
  "external" :> "v1" :> "tasks"
    :> Header "Authorization" Text
    :> QueryParam "user_id" Text
    :> QueryParam "page" Int
    :> QueryParam "page_size" Int
    :> QueryParam "cursor" Text
    :> QueryParam "limit" Int
    :> Get '[JSON] Value
