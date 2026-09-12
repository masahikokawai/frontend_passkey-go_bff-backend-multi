// WebAuthn(パスキー)の options/credential をやり取りする際の、base64url ⇔ ArrayBuffer の素朴な相互変換
//
// 【統合レビューで判明・切り出した理由】
// この2関数はこれまで app/views/home/index.html.erb 内の <script> にインライン定義されており、
// テストコードが1件も無かった(Rails側にJSのテスト基盤が元々存在しなかったため)
//
// base64urlのパディング計算("==".slice(...))は1文字でもズレると
// 実機のWebAuthnの儀式自体が(challengeの中身が化けて)サイレントに失敗する、地味だが
// 壊れやすい処理のため、ここへ切り出しNode組み込みのテストランナー(node --test、追加の依存無し)
// で単体テストできるようにした
// ブラウザ側は importmap-rails 経由でこのファイルをそのまま ESモジュールとして読み込む
// (index.html.erb側は本ファイルをimportするだけにし、実装の二重管理を避ける)
export function base64urlToBuffer(base64url) {
  // 【テスト監査で発見・修正した実バグ】
  // 従来は "==".slice((base64url.length + 3) % 4) というパディング計算式だったが、
  // これは文字列長が3の倍数バイト由来(L%4===0)の場合にしか正しく機能しない
  // (WebAuthnのchallenge・credential id はほぼ常にそれ以外の長さになるため、
  // 実機では高確率でatobが"Invalid character"を投げて壊れていたはずの不具合)
  //
  // 正しい必要パディング数は (4 - (L % 4)) % 4 で、これはL%4が0/2/3のいずれでも成立する
  // (base64の性質上L%4が1になることは無い)
  // node --test で長さ0〜10バイトを全数検証して発見した
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
