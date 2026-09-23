-- | 内部REST v1(:8119)のAPI仕様を、Haskellの型として記述する。
--
-- 【このHaskell実装で最も学習価値の高い設計判断】Servantでは、APIの形("internal/v1/tasks"への
-- GET/POST、"internal/v1/tasks/{id}"へのGET/PATCH/DELETE、それぞれのヘッダ・パラメータ・
-- リクエストボディ・レスポンスの型)を、実行時のルーティングロジックとしてではなく、
-- このモジュールの`TaskAPI`という**型そのもの**として書く。コンパイラは、後述の
-- `Server.hs`で実装するハンドラの型が、ここで宣言したAPIの型と正確に一致することを
-- 静的に検査する(ハンドラの実装を書き忘れたり、返す型を間違えたりすると型エラーになる)。
-- これはJavalin/Ktor/FastAPI+手書きバリデーション/Plug.Routerのような「実行時にルートを
-- 明示的に登録する」設計とは対照的な、Haskellの表現力の高い型システムをWeb API定義に
-- 直接応用した設計である(README.md「アーキテクチャ選定」節参照)。
module BackendHaskell.Rest.Api
  ( TaskAPI
  ) where

import Data.Aeson (Value)
import Data.Text (Text)
import Servant

type TaskAPI =
  "internal" :> "v1" :> "tasks" :> Header "Authorization" Text :> QueryParam "limit" Int :> QueryParam "offset" Int :> Get '[JSON] Value
    :<|> "internal" :> "v1" :> "tasks" :> Header "Authorization" Text :> ReqBody '[JSON] Value :> Verb 'POST 201 '[JSON] Value
    :<|> "internal" :> "v1" :> "tasks" :> Capture "id" Text :> Header "Authorization" Text :> Get '[JSON] Value
    :<|> "internal" :> "v1" :> "tasks" :> Capture "id" Text :> Header "Authorization" Text :> ReqBody '[JSON] Value :> Patch '[JSON] Value
    :<|> "internal" :> "v1" :> "tasks" :> Capture "id" Text :> Header "Authorization" Text :> Verb 'DELETE 204 '[JSON] NoContent
