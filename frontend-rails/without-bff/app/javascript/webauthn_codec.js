// WebAuthn(パスキー)の options/credential をやり取りする際の、base64url ⇔ ArrayBuffer の素朴な相互変換
//
// frontend-rails/with-bffのapp/javascript/webauthn_codec.jsと全く同じ実装(2つのRailsアプリは
// 別プロセス・別アセットパイプラインのため、コードの共有機構が無く同じ内容を複製している)。
// base64urlのパディング計算式の正しさ(L%4が0/2/3のいずれでも成立する(4 - (L % 4)) % 4)は
// with-bff側でnode --testにより長さ0〜10バイトを全数検証済み。
export function base64urlToBuffer(base64url) {
  const padLength = (4 - (base64url.length % 4)) % 4;
  const padded = base64url.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat(padLength);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

export function bufferToBase64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let i = 0; i < bytes.byteLength; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
