-- | backend-java/backend-kotlin/backend-python/backend-elixirのtest_token_helper相当
module TestTokenHelper
  ( makeHmacToken
  , makeHmacTokenWithAzp
  , makeAlgNoneToken
  , generateRsaKeyPair
  , makeRsaToken
  , makeRsaTokenWithAzp
  , publicJwkFields
  ) where

import Crypto.JOSE (runJOSE)
import qualified Crypto.JOSE.JWK as JWK
import qualified Crypto.JWT as JWT
import Control.Lens ((&), (.~), (?~), preview)
import qualified Crypto.PubKey.RSA as RSA
import qualified Data.Aeson as Aeson
import qualified Data.ByteString.Base64.URL as B64URL
import qualified Data.ByteString.Char8 as BC
import qualified Data.ByteString.Lazy as BL
import qualified Data.Map as M
import Data.Text (Text)
import qualified Data.Text as T
import qualified Data.Text.Encoding as TE
import Data.Time (addUTCTime, getCurrentTime)
import Data.Time.Clock.POSIX (utcTimeToPOSIXSeconds)

makeHmacToken :: Text -> Text -> Text -> Text -> Integer -> IO Text
makeHmacToken secret iss aud sub expOffsetSecs = signWith (JWK.fromOctets (TE.encodeUtf8 secret)) JWT.HS256 Nothing iss aud sub expOffsetSecs Nothing

makeRsaToken :: RSA.PrivateKey -> Text -> Text -> Text -> Text -> Integer -> IO Text
makeRsaToken privateKey kid iss aud sub expOffsetSecs = do
  let jwk = privateJwk privateKey kid
  signWith jwk JWT.RS256 (Just kid) iss aud sub expOffsetSecs Nothing

-- | 外部公開APIのRequireExternalClientAuth相当のテスト専用: azp(トークンを取得した
-- OAuth2クライアントのclient_id)を私的クレームとして追加したローカルHMACトークンを作る
makeHmacTokenWithAzp :: Text -> Text -> Text -> Text -> Integer -> Text -> IO Text
makeHmacTokenWithAzp secret iss aud sub expOffsetSecs azp =
  signWith (JWK.fromOctets (TE.encodeUtf8 secret)) JWT.HS256 Nothing iss aud sub expOffsetSecs (Just azp)

-- | 同上、RS256(Keycloak発行相当)版
makeRsaTokenWithAzp :: RSA.PrivateKey -> Text -> Text -> Text -> Text -> Integer -> Text -> IO Text
makeRsaTokenWithAzp privateKey kid iss aud sub expOffsetSecs azp = do
  let jwk = privateJwk privateKey kid
  signWith jwk JWT.RS256 (Just kid) iss aud sub expOffsetSecs (Just azp)

signWith :: JWK.JWK -> JWT.Alg -> Maybe Text -> Text -> Text -> Text -> Integer -> Maybe Text -> IO Text
signWith jwk alg mKid iss aud sub expOffsetSecs mAzp = do
  now <- getCurrentTime
  let expAt = addUTCTime (fromIntegral expOffsetSecs) now
      baseClaims =
        JWT.emptyClaimsSet
          & JWT.claimIss ?~ fromStringOrUri iss
          & JWT.claimSub ?~ fromStringOrUri sub
          & JWT.claimAud ?~ JWT.Audience [fromStringOrUri aud]
          & JWT.claimExp ?~ JWT.NumericDate expAt
      -- azpは標準クレームではなくprivate claim(未登録クレーム)のため、
      -- unregisteredClaimsへ直接差し込む(HmacVerifier/JwksVerifierのazpOfと対になる)
      claims = case mAzp of
        Nothing -> baseClaims
        Just azp -> baseClaims & JWT.unregisteredClaims .~ M.fromList [("azp", Aeson.String azp)]
      header =
        JWT.newJWSHeaderProtected alg
          & JWT.kid .~ fmap (JWT.newHeaderParamProtected) mKid
  result <- runJOSE (JWT.signClaims jwk header claims)
  case result of
    Left err -> error ("TestTokenHelper: failed to sign token: " ++ show (err :: JWT.JWTError))
    Right signedJwt -> pure (TE.decodeUtf8 (BL.toStrict (JWT.encodeCompact signedJwt)))

fromStringOrUri :: Text -> JWT.StringOrURI
fromStringOrUri t = case preview JWT.stringOrUri t of
  Just v -> v
  Nothing -> error ("TestTokenHelper: invalid StringOrURI: " ++ T.unpack t)

-- | "alg":"none"は署名の要らない自己主張トークンになるため、必ず拒否されなければならない。
-- joseは通常algをsigner経由で扱うため、"none"は手動で組み立てる
makeAlgNoneToken :: Text -> Text -> Text -> Integer -> IO Text
makeAlgNoneToken iss aud sub expOffsetSecs = do
  now <- getCurrentTime
  let expAt = round (realToFrac (utcTimeToPOSIXSeconds (addUTCTime (fromIntegral expOffsetSecs) now)) :: Double) :: Integer
      header = Aeson.object ["alg" Aeson..= ("none" :: Text), "typ" Aeson..= ("JWT" :: Text)]
      payload = Aeson.object ["sub" Aeson..= sub, "iss" Aeson..= iss, "aud" Aeson..= aud, "exp" Aeson..= expAt]
      headerB64 = b64urlEncode (BL.toStrict (Aeson.encode header))
      payloadB64 = b64urlEncode (BL.toStrict (Aeson.encode payload))
  pure (headerB64 <> "." <> payloadB64 <> ".")

b64urlEncode :: BC.ByteString -> Text
b64urlEncode = TE.decodeUtf8 . BC.dropWhileEnd (== '=') . B64URL.encode

generateRsaKeyPair :: IO (RSA.PublicKey, RSA.PrivateKey)
generateRsaKeyPair = RSA.generate 256 65537

privateJwk :: RSA.PrivateKey -> Text -> JWK.JWK
privateJwk priv kid =
  JWK.fromKeyMaterial (JWK.RSAKeyMaterial (JWK.toRSAKeyParameters priv))
    & JWK.jwkKid ?~ kid

-- | JWKSモックサーバーへ登録する公開鍵の"n"/"e"フィールド(base64url、パディング無し)
publicJwkFields :: RSA.PublicKey -> (Text, Text)
publicJwkFields pub = (b64urlUint (RSA.public_n pub), b64urlUint (RSA.public_e pub))

b64urlUint :: Integer -> Text
b64urlUint n = b64urlEncode (integerToBytes n)

integerToBytes :: Integer -> BC.ByteString
integerToBytes n = BC.pack (map toEnum (go n []))
  where
    go 0 acc = if null acc then [0] else acc
    go x acc = go (x `div` 256) (fromIntegral (x `mod` 256) : acc)
