-- | ローカルHMAC発行(iss=bff-gin-local-hmac)の検証。HS256、共有シークレット。
-- backend-java/backend-kotlin/backend-python/backend-elixirと同じ「ライブラリに全てを
-- 任せず、境界の検証は自分の目でも確認する」多層防御の設計: joseによる署名検証に加えて、
-- ヘッダのalgが期待するアルゴリズムと完全一致することも独立して明示的に確認する
-- (アルゴリズム混同攻撃対策)。
--
-- 【Haskellの`IO`モナドについて、backend-scala-http4sとの対比】この関数はネットワーク越しの
-- 検証こそ行わないが(HMACはローカルな計算のみ)、JwksVerifier(後述)はHTTPでJWKSを取得する
-- 副作用を伴う。Haskellでは副作用を行う関数の型に必ず`IO`が現れ、コンパイラがこれを
-- 強制する(IOを経由しない限り副作用のあるコードを呼び出すこと自体ができない)。これは
-- backend-scala-http4sが`cats.effect.IO`で同じ役割を果たしているのと概念的に同じ発想である
-- (backend-scala-http4s/src/main/scala/com/bffgin/backend/TaskRepo.scalaのIO[...]と対比、
-- 詳細はREADME.md「アーキテクチャ選定」節のIOモナドに関する節を参照)。
module BackendHaskell.Auth.HmacVerifier
  ( newHmacVerifier
  ) where

import qualified Crypto.JOSE.JWK as JWK
import qualified Crypto.JWT as JWT
import Control.Lens ((&), (.~), (^.), review)
import Control.Monad.Except (ExceptT, runExceptT)
import qualified Data.Aeson as Aeson
import qualified Data.Aeson.KeyMap as KM
import qualified Data.ByteString.Lazy as BL
import qualified Data.Map as M
import Data.Set (fromList)
import Data.Text (Text)
import qualified Data.Text.Encoding as TE

import BackendHaskell.Auth.Claims (Claims (..))
import BackendHaskell.Auth.Dispatcher (VerifierFn, decodeB64Url, splitToken)

newHmacVerifier :: Text -> Text -> Text -> VerifierFn
newHmacVerifier secret issuer audience token = do
  case checkHeaderAlg token of
    Left err -> pure (Left err)
    Right () -> verifySignatureAndClaims secret issuer audience token

-- | 【セキュリティ上のバッド/グッドプラクティス、backend-java/backend-kotlin/backend-python/
-- backend-elixirと同じ観点】joseの`verifyClaims`はJWKに設定したアルゴリズム系統でのみ検証を
-- 試みるため、algが違えば署名検証自体が自然に失敗するはずだが、アルゴリズム混同攻撃への
-- 多層防御として、ヘッダのalgが期待するアルゴリズム(HS256)と完全一致することも
-- 署名検証の「前」に独立して明示的に確認する。この事前チェックを省略した場合、
-- 悪意ある"alg":"none"トークンや、RS256用の公開鍵をHS256の共有シークレットとして誤用させる
-- 攻撃の可能性を、ライブラリの内部実装の詳細にのみ依存して防ぐことになってしまう
checkHeaderAlg :: Text -> Either Text ()
checkHeaderAlg token = do
  (headerB64, _, _) <- splitToken token
  raw <- decodeB64Url headerB64
  obj <- maybe (Left "malformed_token") Right (Aeson.decodeStrict raw :: Maybe Aeson.Object)
  case KM.lookup "alg" obj of
    Just (Aeson.String "HS256") -> Right ()
    _ -> Left "unexpected_alg"

verifySignatureAndClaims :: Text -> Text -> Text -> Text -> IO (Either Text Claims)
verifySignatureAndClaims secret issuer audience token = do
  result <- runExceptT $ do
    let jwk = JWK.fromOctets (TE.encodeUtf8 secret)
    signedJwt <- JWT.decodeCompact (BL.fromStrict (TE.encodeUtf8 token)) :: ExceptT JWT.JWTError IO JWT.SignedJWT
    -- exp/nbfの期限検証はverifyClaims自身が現在時刻(IOのMonadTimeインスタンス)と突き合わせて
    -- 行う。audiencePredicateは常にTrueにしておき、aud/issの一致確認は署名検証成功後、
    -- 自前のcheckClaimsAndBuildで明示的に行う(他言語と同じ多層防御、joseの内部実装詳細に
    -- 依存しない設計)
    let settings = JWT.defaultJWTValidationSettings (const True) & JWT.algorithms .~ fromList [JWT.HS256]
    JWT.verifyClaims settings jwk signedJwt
  case result of
    Left _ -> pure (Left "signature_verification_failed")
    Right claims -> pure (checkClaimsAndBuild claims issuer audience)

checkClaimsAndBuild :: JWT.ClaimsSet -> Text -> Text -> Either Text Claims
checkClaimsAndBuild claims expectedIssuer expectedAudience = do
  issText <- maybe (Left "unexpected_issuer") (Right . stringOrUriToText) (claims ^. JWT.claimIss)
  if issText /= expectedIssuer then Left "unexpected_issuer" else Right ()
  audOk <- Right (audienceMatches (claims ^. JWT.claimAud) expectedAudience)
  if not audOk then Left "unexpected_audience" else Right ()
  subText <- maybe (Left "missing_sub") (Right . stringOrUriToText) (claims ^. JWT.claimSub)
  Right (Claims {claimsSub = subText, claimsIss = issText, claimsAzp = azpOf claims})

-- | azpは標準クレームではなくprivate claim(未登録クレーム)のため、jose自身のレンズには
-- 用意されておらず`unregisteredClaims`(HashMap Text Value)から自分で取り出す。
-- 外部公開API(Phase 2)のRequireExternalClientAuth相当の検証専用で、内部REST/gRPCの
-- user_id解決では使わない(欠落時は空文字、azp一致検証は必ず失敗する側に倒れる)
azpOf :: JWT.ClaimsSet -> Text
azpOf claims = case M.lookup "azp" (claims ^. JWT.unregisteredClaims) of
  Just (Aeson.String azp) -> azp
  _ -> ""

-- | `Show`の派生実装は`Arbitrary "..."`のようにコンストラクタ名や引用符を含めてしまうため、
-- 期待するissuer/audienceの生テキストと単純比較できない。`stringOrUri`プリズムの
-- reviewの向き(StringOrURI -> Text)が、元のテキスト表現へ正しく戻す変換になっている
stringOrUriToText :: JWT.StringOrURI -> Text
stringOrUriToText = review JWT.stringOrUri

audienceMatches :: Maybe JWT.Audience -> Text -> Bool
audienceMatches Nothing _ = False
audienceMatches (Just (JWT.Audience auds)) expected =
  expected `elem` map stringOrUriToText auds
