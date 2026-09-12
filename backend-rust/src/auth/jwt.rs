//! backend(Go)の internal/authjwt パッケージ(dispatcher.go/jwks.go/hmac.go)の再現。
//! Keycloak発行(JWKS/RS256)・ローカルHMAC発行(HS256)・ローカルRSA発行(JWKS/RS256、
//! bffの/.well-known/jwks.jsonから取得)の3issuerを、tokenのiss(署名検証前に覗いた値)で
//! 振り分ける2段構造(Dispatcher)も同じにしている。

use jsonwebtoken::{decode, decode_header, Algorithm, DecodingKey, Validation};
use serde::Deserialize;
use std::collections::HashMap;
use std::sync::RwLock;

pub const LOCAL_HMAC_ISSUER: &str = "bff-gin-local-hmac";
pub const LOCAL_RSA_ISSUER: &str = "bff-gin-local-rsa";

pub fn is_local_issuer(iss: &str) -> bool {
    iss == LOCAL_HMAC_ISSUER || iss == LOCAL_RSA_ISSUER
}

#[derive(Debug, Deserialize, Clone, Default)]
pub struct RealmAccess {
    #[serde(default)]
    pub roles: Vec<String>,
}

#[derive(Debug, Deserialize, Clone)]
pub struct Claims {
    pub sub: String,
    pub iss: String,
    #[serde(default)]
    pub exp: i64,
    #[serde(default)]
    pub nbf: Option<i64>,
    #[serde(default)]
    pub aud: Option<serde_json::Value>,
    #[serde(default)]
    pub preferred_username: String,
    #[serde(default)]
    pub email: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub realm_access: RealmAccess,
    #[serde(default)]
    pub azp: String,
}

#[derive(Debug, Deserialize)]
struct UnverifiedIssuer {
    iss: String,
}

#[derive(Debug, thiserror::Error)]
pub enum VerifyError {
    #[error("JWTのパースに失敗(iss確認前): {0}")]
    Parse(String),
    #[error("不明なissuer: {0:?}")]
    UnknownIssuer(String),
    #[error("JWT検証失敗: {0}")]
    Verify(String),
}

#[async_trait::async_trait]
pub trait TokenVerifier: Send + Sync {
    async fn verify(&self, token: &str) -> Result<Claims, VerifyError>;
}

/// HMACVerifier はローカル認証HMAC版(iss=LOCAL_HMAC_ISSUER)の検証
pub struct HmacVerifier {
    secret: String,
    issuer: String,
    audience: String,
}

impl HmacVerifier {
    pub fn new(secret: impl Into<String>, issuer: impl Into<String>, audience: impl Into<String>) -> Self {
        Self { secret: secret.into(), issuer: issuer.into(), audience: audience.into() }
    }
}

#[async_trait::async_trait]
impl TokenVerifier for HmacVerifier {
    async fn verify(&self, token: &str) -> Result<Claims, VerifyError> {
        let mut validation = Validation::new(Algorithm::HS256);
        validation.set_issuer(&[&self.issuer]);
        validation.set_audience(&[&self.audience]);
        let key = DecodingKey::from_secret(self.secret.as_bytes());
        let data = decode::<Claims>(token, &key, &validation)
            .map_err(|e| VerifyError::Verify(format!("HMAC: {}", e)))?;
        Ok(data.claims)
    }
}

/// JwksVerifier はKeycloak/ローカルRSA共通のJWKSベース検証
/// kid(鍵ID)ごとにDecodingKeyをキャッシュし、未知のkidが来たときだけJWKS再取得する
/// (backend(Go)のjwks.goと同じ「kid不一致時のみ再取得」戦略)
pub struct JwksVerifier {
    jwks_url: String,
    issuer: String,
    audience: String,
    http: reqwest::Client,
    keys: RwLock<HashMap<String, DecodingKey>>,
}

#[derive(Debug, Deserialize)]
struct JwkKey {
    kty: String,
    kid: String,
    #[serde(default)]
    r#use: String,
    n: String,
    e: String,
}

#[derive(Debug, Deserialize)]
struct JwksResponse {
    keys: Vec<JwkKey>,
}

impl JwksVerifier {
    pub fn new(jwks_url: impl Into<String>, issuer: impl Into<String>, audience: impl Into<String>) -> Self {
        Self {
            jwks_url: jwks_url.into(),
            issuer: issuer.into(),
            audience: audience.into(),
            http: reqwest::Client::new(),
            keys: RwLock::new(HashMap::new()),
        }
    }

    async fn refresh(&self) -> Result<(), VerifyError> {
        let resp = self
            .http
            .get(&self.jwks_url)
            .send()
            .await
            .map_err(|e| VerifyError::Verify(format!("JWKS取得失敗: {}", e)))?;
        let parsed: JwksResponse = resp
            .json()
            .await
            .map_err(|e| VerifyError::Verify(format!("JWKS JSONパース失敗: {}", e)))?;

        let mut new_keys = HashMap::new();
        for k in parsed.keys {
            if k.kty != "RSA" || (!k.r#use.is_empty() && k.r#use != "sig") {
                continue;
            }
            if let Ok(key) = DecodingKey::from_rsa_components(&k.n, &k.e) {
                new_keys.insert(k.kid, key);
            }
        }
        *self.keys.write().unwrap() = new_keys;
        Ok(())
    }

    fn lookup(&self, kid: &str) -> Option<DecodingKey> {
        self.keys.read().unwrap().get(kid).cloned()
    }
}

#[async_trait::async_trait]
impl TokenVerifier for JwksVerifier {
    async fn verify(&self, token: &str) -> Result<Claims, VerifyError> {
        let header = decode_header(token).map_err(|e| VerifyError::Verify(format!("ヘッダ解析失敗: {}", e)))?;
        let kid = header.kid.ok_or_else(|| VerifyError::Verify("JWTヘッダにkidが無い".to_string()))?;

        let key = match self.lookup(&kid) {
            Some(k) => k,
            None => {
                self.refresh().await?;
                self.lookup(&kid).ok_or_else(|| VerifyError::Verify(format!("kid={} に対応する公開鍵が見つからない", kid)))?
            }
        };

        let mut validation = Validation::new(Algorithm::RS256);
        validation.set_issuer(&[&self.issuer]);
        validation.set_audience(&[&self.audience]);
        let data = decode::<Claims>(token, &key, &validation)
            .map_err(|e| VerifyError::Verify(format!("RSA/JWKS: {}", e)))?;
        Ok(data.claims)
    }
}

/// Dispatcher はJWTの`iss`クレームで検証方式(Keycloak/ローカルHMAC/ローカルRSA)を振り分ける
/// (backend(Go)のauthjwt.Dispatcherと同じ2段構造。issの詐称は委譲先の署名検証で弾かれる)
pub struct Dispatcher {
    by_issuer: HashMap<String, Box<dyn TokenVerifier>>,
}

impl Dispatcher {
    pub fn new() -> Self {
        Self { by_issuer: HashMap::new() }
    }

    pub fn register(mut self, issuer: impl Into<String>, verifier: Box<dyn TokenVerifier>) -> Self {
        self.by_issuer.insert(issuer.into(), verifier);
        self
    }

    pub async fn verify(&self, token: &str) -> Result<Claims, VerifyError> {
        // 署名検証前にissだけ覗く(誰でも書き換えられる値。ここでは振り分け先の
        // 決定にのみ使い、実際の信頼は委譲先verifierの署名検証に委ねる)
        let parts: Vec<&str> = token.split('.').collect();
        if parts.len() != 3 {
            return Err(VerifyError::Parse("JWT形式が不正".to_string()));
        }
        let payload = base64::Engine::decode(&base64::engine::general_purpose::URL_SAFE_NO_PAD, parts[1])
            .map_err(|e| VerifyError::Parse(e.to_string()))?;
        let peek: UnverifiedIssuer =
            serde_json::from_slice(&payload).map_err(|e| VerifyError::Parse(e.to_string()))?;

        let verifier = self
            .by_issuer
            .get(&peek.iss)
            .ok_or_else(|| VerifyError::UnknownIssuer(peek.iss.clone()))?;
        verifier.verify(token).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use jsonwebtoken::{encode, EncodingKey, Header};
    use serde::Serialize;

    #[test]
    fn is_local_issuer_matches_hmac_and_rsa_only() {
        assert!(is_local_issuer(LOCAL_HMAC_ISSUER));
        assert!(is_local_issuer(LOCAL_RSA_ISSUER));
        assert!(!is_local_issuer("http://localhost:8082/realms/training"));
        assert!(!is_local_issuer(""));
    }

    #[derive(Serialize)]
    struct RawClaims<'a> {
        sub: &'a str,
        iss: &'a str,
        aud: &'a str,
        exp: i64,
    }

    fn make_hmac_token(secret: &str, iss: &str, aud: &str, sub: &str, exp_offset_secs: i64) -> String {
        let claims = RawClaims {
            sub,
            iss,
            aud,
            exp: (chrono::Utc::now().timestamp() + exp_offset_secs),
        };
        encode(&Header::new(jsonwebtoken::Algorithm::HS256), &claims, &EncodingKey::from_secret(secret.as_bytes())).unwrap()
    }

    // backend(Go)のHMACVerifier(hmac_test.go相当)と同じ観点: 正しい秘密鍵・iss・audなら
    // 検証が通り、subがそのままclaims.subへ伝わる
    #[tokio::test]
    async fn hmac_verifier_accepts_valid_token() {
        let token = make_hmac_token("test-secret", LOCAL_HMAC_ISSUER, "backend", "42", 3600);
        let verifier = HmacVerifier::new("test-secret", LOCAL_HMAC_ISSUER, "backend");
        let claims = verifier.verify(&token).await.expect("verify should succeed");
        assert_eq!(claims.sub, "42");
        assert_eq!(claims.iss, LOCAL_HMAC_ISSUER);
    }

    #[tokio::test]
    async fn hmac_verifier_rejects_wrong_secret() {
        let token = make_hmac_token("test-secret", LOCAL_HMAC_ISSUER, "backend", "42", 3600);
        let verifier = HmacVerifier::new("different-secret", LOCAL_HMAC_ISSUER, "backend");
        assert!(verifier.verify(&token).await.is_err());
    }

    #[tokio::test]
    async fn hmac_verifier_rejects_expired_token() {
        let token = make_hmac_token("test-secret", LOCAL_HMAC_ISSUER, "backend", "42", -3600);
        let verifier = HmacVerifier::new("test-secret", LOCAL_HMAC_ISSUER, "backend");
        assert!(verifier.verify(&token).await.is_err());
    }

    #[tokio::test]
    async fn hmac_verifier_rejects_wrong_audience() {
        let token = make_hmac_token("test-secret", LOCAL_HMAC_ISSUER, "someone-else", "42", 3600);
        let verifier = HmacVerifier::new("test-secret", LOCAL_HMAC_ISSUER, "backend");
        assert!(verifier.verify(&token).await.is_err());
    }

    // Dispatcherがissuerで正しいverifierへ振り分けることを確認する
    // (backend(Go)のauthjwt.Dispatcherと同じ2段構造)
    #[tokio::test]
    async fn dispatcher_routes_by_issuer_and_rejects_unknown_issuer() {
        let dispatcher = Dispatcher::new().register(
            LOCAL_HMAC_ISSUER,
            Box::new(HmacVerifier::new("test-secret", LOCAL_HMAC_ISSUER, "backend")),
        );

        let good_token = make_hmac_token("test-secret", LOCAL_HMAC_ISSUER, "backend", "7", 3600);
        let claims = dispatcher.verify(&good_token).await.expect("known issuer should route correctly");
        assert_eq!(claims.sub, "7");

        let unknown_issuer_token = make_hmac_token("test-secret", "unknown-issuer", "backend", "7", 3600);
        let err = dispatcher.verify(&unknown_issuer_token).await.unwrap_err();
        assert!(matches!(err, VerifyError::UnknownIssuer(_)));
    }
}
