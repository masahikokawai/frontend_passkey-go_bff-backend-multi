-- | Keycloak発行・ローカルRSA発行(iss=bff-gin-local-rsa)共通のJWKSベース検証。RS256。
-- kidごとの公開鍵キャッシュは自分では持たず、STMベースのJwksCacheに問い合わせる。
-- 未知のkidが来たときだけ再取得を依頼する「kid不一致時のみ再取得」戦略は他言語と同じ。
module BackendHaskell.Auth.JwksVerifier
  ( newJwksVerifier
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
import BackendHaskell.Auth.JwksCache (JwksCache, lookupKey, refresh)

newJwksVerifier :: JwksCache -> Text -> Text -> VerifierFn
newJwksVerifier cache issuer audience token = do
  case checkHeaderAlgAndKid token of
    Left err -> pure (Left err)
    Right kid -> do
      keyResult <- lookupOrRefresh cache kid
      case keyResult of
        Left err -> pure (Left err)
        Right jwk -> verifySignatureAndClaims jwk issuer audience token

checkHeaderAlgAndKid :: Text -> Either Text Text
checkHeaderAlgAndKid token = do
  (headerB64, _, _) <- splitToken token
  raw <- decodeB64Url headerB64
  obj <- maybe (Left "malformed_token") Right (Aeson.decodeStrict raw :: Maybe Aeson.Object)
  case KM.lookup "alg" obj of
    Just (Aeson.String "RS256") -> Right ()
    _ -> Left "unexpected_alg"
  case KM.lookup "kid" obj of
    Just (Aeson.String kid) -> Right kid
    _ -> Left "missing_kid"

lookupOrRefresh :: JwksCache -> Text -> IO (Either Text JWK.JWK)
lookupOrRefresh cache kid = do
  found <- lookupKey cache kid
  case found of
    Just jwk -> pure (Right jwk)
    Nothing -> do
      refreshResult <- refresh cache
      case refreshResult of
        Left err -> pure (Left err)
        Right () -> do
          found2 <- lookupKey cache kid
          pure (maybe (Left "unknown_kid") Right found2)

verifySignatureAndClaims :: JWK.JWK -> Text -> Text -> Text -> IO (Either Text Claims)
verifySignatureAndClaims jwk issuer audience token = do
  result <- runExceptT $ do
    signedJwt <- JWT.decodeCompact (BL.fromStrict (TE.encodeUtf8 token)) :: ExceptT JWT.JWTError IO JWT.SignedJWT
    let settings = JWT.defaultJWTValidationSettings (const True) & JWT.algorithms .~ fromList [JWT.RS256]
    JWT.verifyClaims settings jwk signedJwt
  case result of
    Left _ -> pure (Left "signature_verification_failed")
    Right claims -> pure (checkClaimsAndBuild claims issuer audience)

checkClaimsAndBuild :: JWT.ClaimsSet -> Text -> Text -> Either Text Claims
checkClaimsAndBuild claims expectedIssuer expectedAudience = do
  issText <- maybe (Left "unexpected_issuer") (Right . stringOrUriToText) (claims ^. JWT.claimIss)
  if issText /= expectedIssuer then Left "unexpected_issuer" else Right ()
  if not (audienceMatches (claims ^. JWT.claimAud) expectedAudience) then Left "unexpected_audience" else Right ()
  subText <- maybe (Left "missing_sub") (Right . stringOrUriToText) (claims ^. JWT.claimSub)
  Right (Claims {claimsSub = subText, claimsIss = issText, claimsAzp = azpOf claims})

-- | HmacVerifier.hsと同じ理由(azpは未登録クレームのためjoseのレンズには無い)
azpOf :: JWT.ClaimsSet -> Text
azpOf claims = case M.lookup "azp" (claims ^. JWT.unregisteredClaims) of
  Just (Aeson.String azp) -> azp
  _ -> ""

-- | HmacVerifier.hsと同じ理由(`Show`派生実装のコンストラクタ表記を避けるため)、
-- `stringOrUri`プリズムのreview方向で元のテキスト表現へ変換する
stringOrUriToText :: JWT.StringOrURI -> Text
stringOrUriToText = review JWT.stringOrUri

audienceMatches :: Maybe JWT.Audience -> Text -> Bool
audienceMatches Nothing _ = False
audienceMatches (Just (JWT.Audience auds)) expected =
  expected `elem` map stringOrUriToText auds
