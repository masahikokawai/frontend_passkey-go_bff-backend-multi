このプロジェクトのテーマ(多言語/多フレームワーク比較、BFFパターン、Feature Flag学習)に沿ったものを挙げます。

1. Task検索・フィルタのUI強化(現状はステータス/ラベルの絞り込みのみ。フリーテキスト検索・期限範囲指定などを5言語で比較実装)
2. 通知機能(期限が近いタスクをメール/画面内通知。バックグラウンドジョブの学習例としてGo(goroutine)/Rust(tokio)/Scala(cats-effect fiber)/Rails(Sidekiq相当)の比較になる)
3. 監査ログの横断検索・可視化画面(現状admin/go・admin/railsに個別のaudit log画面はあるが、Feature Flag変更履歴を横断的に見る画面は無い)
4. OpenTelemetry導入(今回のセッションでbackendの5言語にログを揃えたので、次の自然な発展として分散トレーシングの比較学習)
5. Rate Limiting実装(§23.2で「意図的に見送った」と明記されている既知の制約。IPアドレス/アカウント単位の制限を5言語+bffで比較実装)
6. CSV/Excelエクスポート: タスク一覧の出力機能。ファイル生成・ストリーミング処理を5言語で比較(Goencoding/csv、Rustcsvcrate、Scala fs2、Rails CSV)。今回のボディサイズ上限の議論と対になる「大きなレスポンスの扱い」の学習にもなる
7. 一括操作(bulk delete/一括ステータス変更): トランザクション処理・バッチクエリの比較。既存のREST v1(N+1)/gRPC v2(Preload)という設計テーマの延長として自然
8. タスクのコメント・変更履歴: 1対多の新しい関連を追加し、Feature Flag監査ログとは別の「業務データの履歴管理」パターンを学習
9. リアルタイム更新(WebSocket/SSE): 他ユーザー/別タブでの変更を一覧へ即時反映。gRPCの双方向ストリーミングとの比較も込みで5言語実装できると学習効果が高い
10. マルチテナント/組織対応: タスクをユーザー個人ではなく組織に紐付け。今までのIDOR系セキュリティ監査をさらに深掘りする良い題材になる
11. GraphQL APIの追加: 既にbackend.task-protocolでREST/gRPCを比較しているので、その延長で3つ目のプロトコル軸として追加
12. 全文検索エンジンの導入(Elasticsearch/Meilisearch): 現状のSQL LIKE検索との比較。新しいdocker-composeサービスの学習にもなる
13. UIの多言語化(i18n): 日本語/英語切り替え。現状backendのエラーメッセージが日本語ハードコードなので、エラーコード方式への設計変更を伴う良い議論になる
14. ヘルスチェック/readinessエンドポイントの標準化: 5言語+bff+admin全てに統一形式で追加。今回ログを揃えた流れの延長として「本番運用の準備」を学ぶモジュールになる
15. ファイル添付機能: タスクにファイルを添付。MinIO等のオブジェクトストレージをdocker-composeに追加し、アップロード/ストリーミング処理を5言語で比較。今回議論したボディサイズ上限の延長線上の題材
16. 繰り返しタスク/スケジューリング: cron的な定期実行。Goのcronライブラリ・Rustのtokio-cron-scheduler・Scalaのcats-effectベーススケジューラ・Railsのwhenever/Sidekiqを比較
17. 外部APIクライアントごとのレート制限/クォータ: 現状のexternal-api-client(Client Credentials Grant)の仕組みを拡張し、クライアントごとの呼び出し回数制限を追加
18. ダークモード切り替え: 軽量なUI機能。それ自体をfrontend.dark-modeのようなFeature Flagの実例にできる
19. タスクに優先度フィールド追加+ソート: 「5言語全部にカラムを1つ追加する」という一連の変更フローを体験する題材として分かりやすい
20. タスクの論理削除(soft delete): 現状の物理削除からdeleted_at方式へ変更し「削除の取り消し」が可能に。今回のDELETE冪等性監査の延長として相性が良い
21. Webhook通知: タスク変更時に外部URLへPOSTする機能。ユーザー指定URLへのリクエストになるため、既に見つけたOpen Redirect系の知見を活かしたSSRF対策の題材にもなる
22. APIキー管理のセルフサービスUI: 現状のKeycloak Client Credentials Grantとは別の、もう一つの外部認証パターンとして比較
23. タスクの依存関係(A完了until Bに着手不可): リレーショナルモデリング+循環検出アルゴリズムの5言語比較
24. メトリクス(/metrics)エンドポイント: 今回リクエストログを5言語で揃えた流れの延長として、Prometheus形式のメトリクス公開を追加
25. 二要素認証(TOTP): パスキーとは別の追加認証手段。既存のパスキー機能と並ぶ「もう一つの多要素認証」として5言語/フレームワークのTOTPライブラリを比較
26. アカウント設定画面: パスワード変更・アクティブセッション一覧表示・特定セッションの強制ログアウト。既存のRedisセッション管理の上に自然に乗る機能
27. データエクスポート(GDPR的な「自分のデータを取得」機能): タスク・ラベル・監査ログをまとめてJSON/zipで出力。非同期ジョブ+ストリーミング圧縮の言語比較になる
28. タスクテンプレート: よく使うタスクの雛形を登録し、ワンクリックで複製生成
29. カンバンボード表示(ドラッグ&ドロップでステータス変更): フロントエンド寄りの機能。楽観的UI更新の学習題材になり、リアルタイム更新(#9)と組み合わせると相性が良い
30. Task検証ルールのプラグイン機構: カスタムバリデーションルールを差し込める設計。Go(interface)・Rust(trait)・Scala(type class)・Ruby(module)という各言語の拡張性の違いを見せる題材
31. DBバックアップ/リストア機能: admin画面からダンプ出力・復元。運用寄りの学習テーマ
32. ダッシュボード/アクティビティフィード: 期限が近いタスク・最近変更されたタスク等を集約表示。CQRS的な「読み取り専用モデル」の比較学習になる
33. カオスエンジニアリング的な障害注入Feature Flag: 特定言語のbackendにランダムな遅延/エラーを注入するFlagを追加し、bff/frontendの耐障害性を試す。既存のe2e resilienceシナリオ・Feature Flag基盤ともよく噛み合う
34. パーセンテージ単位のカナリアリリース(段階的ロールアウト): 現状on/off・多値選択のFeature Flagを、パーセンテージベースのターゲティングへ拡張。Feature Flagの成熟度を一段上げる自然な発展


今回(第10ラウンド)は、これまで9ラウンドかけて追加してきた大量のテストコード自体の品質(フレーキーさ・リソースリーク・後始末の確実性)を監査するという、初めての角度で進めます。「製品のバグ探し」ではなく「テストコード自体の信頼性」に焦点を当てます。



=============================================================


1. Task検索・フィルタのUI強化(現状はステータス/ラベルの絞り込みのみ。フリーテキスト検索・期限範囲指定などを5言語で比較実装)
2. 通知機能(期限が近いタスクをメール/画面内通知。バックグラウンドジョブの学習例としてGo(goroutine)/Rust(tokio)/Scala(cats-effect fiber)/Rails(Sidekiq相当)の比較になる)
3. 監査ログの横断検索・可視化画面(現状admin/go・admin/railsに個別のaudit log画面はあるが、Feature Flag変更履歴を横断的に見る画面は無い)
4. OpenTelemetry導入(今回のセッションでbackendの5言語にログを揃えたので、次の自然な発展として分散トレーシングの比較学習)
5. CSV/Excelエクスポート: タスク一覧の出力機能。ファイル生成・ストリーミング処理を5言語で比較(Goencoding/csv、Rustcsvcrate、Scala fs2、Rails CSV)。今回のボディサイズ上限の議論と対になる「大きなレスポンスの扱い」の学習にもなる
6. 一括操作(bulk delete/一括ステータス変更): トランザクション処理・バッチクエリの比較。既存のREST v1(N+1)/gRPC v2(Preload)という設計テーマの延長として自然
7. タスクのコメント・変更履歴: 1対多の新しい関連を追加し、Feature Flag監査ログとは別の「業務データの履歴管理」パターンを学習
8. リアルタイム更新(WebSocket/SSE): 他ユーザー/別タブでの変更を一覧へ即時反映。gRPCの双方向ストリーミングとの比較も込みで5言語実装できると学習効果が高い
9. 全文検索エンジンの導入(Elasticsearch/Meilisearch): 現状のSQL LIKE検索との比較。新しいdocker-composeサービスの学習にもなる
10. ファイル添付機能: タスクにファイルを添付。MinIO等のオブジェクトストレージをdocker-composeに追加し、アップロード/ストリーミング処理を5言語で比較。今回議論したボディサイズ上限の延長線上の題材。複数画像の同時アップロード
11. タスクの依存関係(A完了until Bに着手不可): リレーショナルモデリング+循環検出アルゴリズムの5言語比較
12. メトリクス(/metrics)エンドポイント: 今回リクエストログを5言語で揃えた流れの延長として、Prometheus形式のメトリクス公開を追加
13. 二要素認証(TOTP): パスキーとは別の追加認証手段。既存のパスキー機能と並ぶ「もう一つの多要素認証」として5言語/フレームワークのTOTPライブラリを比較
14. アカウント設定画面: パスワード変更・アクティブセッション一覧表示・特定セッションの強制ログアウト。既存のRedisセッション管理の上に自然に乗る機能
15. カンバンボード表示(ドラッグ&ドロップでステータス変更): フロントエンド寄りの機能。楽観的UI更新の学習題材になり、リアルタイム更新(#9)と組み合わせると相性が良い
16. Task検証ルールのプラグイン機構: カスタムバリデーションルールを差し込める設計。Go(interface)・Rust(trait)・Scala(type class)・Ruby(module)という各言語の拡張性の違いを見せる題材
17. ダッシュボード/アクティビティフィード: 期限が近いタスク・最近変更されたタスク等を集約表示。CQRS的な「読み取り専用モデル」の比較学習になる
18. カオスエンジニアリング的な障害注入Feature Flag: 特定言語のbackendにランダムな遅延/エラーを注入するFlagを追加し、bff/frontendの耐障害性を試す。既存のe2e resilienceシナリオ・Feature Flag基盤ともよく噛み合う
19. パーセンテージ単位のカナリアリリース(段階的ロールアウト): 現状on/off・多値選択のFeature Flagを、パーセンテージベースのターゲティングへ拡張。Feature Flagの成熟度を一段上げる自然な発展
20. 19 をマルチテナント単位で制御、company_id などの単位で制御を入れられるように
21. toxiproxy を使って、障害シュミレート
22. サーキットブレーカーを各言語で用意
23. OpenTelemetryの TraceId
分散トレーシングにおいて一連の関連する処理（リクエストなど）全体を一意に識別するための32桁の16進数（16バイト配列）のID
24. kubernetes(k8s)
25. Rust/Go での低レイヤー
```text
以下、大きく2点

①点目

■Kubernetes(k8s) や Rust/Go による低レイヤーの学びのステップ
===================================================
①Rustの基礎固め
ownership
borrowing
lifetime
Result / Option
enum / match
trait
Box
Rc / Arc
RefCell
Mutex / RwLock
Send / Sync

②RustでHTTP Serverを自作
1. TCP Echo Server
2. HTTP requestを受信
3. HTTP Parser
4. Response生成
5. Router
6. Thread per connection
7. Thread Pool
8. Keep-Alive
9. async化

（ソケット通信、バッファ管理、所有権、Arc/Mutex）
ネットワーク（入門〜中級）

作るものRustで学べることTCP Echo → HTTPサーバー（std::netのみ）→ Router → スレッドプール → asyncソケット、所有権、Arc/Mutex、Send/SyncDNSクライアント（UDP）バイナリのパース、&[u8]、Result/Option非同期ランタイム自作（epoll/kqueue）Future / Pin / Waker、async/awaitの仕組みユーザー空間TCP（TUN/TAP）パケット処理、状態遷移、再送制御

③Redis風KVS
段階的に育てていく題材
TCPサーバー → プロトコルパーサー（RESP） → HashMapでのKV → 複数クライアント・並行処理 → TTL → WAL → Snapshot → クラッシュリカバリ
この1プロジェクトで、ネットワーク・メモリ・所有権・並行処理・async・ファイルIOを一通り扱える
写経ではなく自分で仕様を決めて作り、詰まったときだけ参考にするのがポイント

並行処理・asyncを理解する
複数クライアントへと対応していく
共有 HashMap を Mutex で保護する実装や、message passing という設計を扱う

④自作DB・ストレージエンジン
段階: KVS → ファイル永続化 + WAL → B+Tree / LSM-Tree → ページ管理・バッファプール → SQLパーサー → Planner → Executor
B+Treeだけ単体で実装しても、Box・Rc・RefCell・再帰型と格闘する
データ構造、ファイルI/O、メモリ管理、並行処理がすべて詰まった低レイヤー学習の決定版
B+Treeを単独で作る

⑤Memory Allocator / OS
https://os.phil-opp.com/ja/
メモリアロケータ：free list → first fit / best fit → buddy allocator。unsafe、生ポインタ、NonNull、Layout、アラインメントを学べる
自作OS：no_stdでQEMU上にブート → 割り込み → メモリ管理 → スケジューラ
ドライバ：ハードウェア知識の比重が急に上がり、Rustの学習というよりカーネル・ハードの学習になりがち。最初には非推奨で、触るならマイコンから

⑥自作コンテナランタイム（Kubernetesへの架け橋）
Linuxのカーネル機能を用いてコンテナを立ち上げる仕組みを理解し、Kubernetes（containerd/runc）の裏側を解き明かす
ここは Goでやるのもかなりおすすめ

⑦OS自作・組み込み・低レベルメモリ管理 or GoでKubernetes Controllerを自作 or TCPをRustで自作
===================================================

===================================================
【メインルート】
Phase 0  Rust基礎                        (Rust)
Phase 1  HTTP Server 自作                (Rust)  ─┐
Phase 2  Mini Redis + 並行処理/async      (Rust)   │ 1つのリポジトリで育てる
Phase 3  永続化（WAL/Snapshot/Recovery）  (Rust)   │ rust-system-lab
Phase 4  DB / Storage Engine（LSM / B+Tree）   (Rust)  ─┘
   ↓ ── ここで言語を切り替える ──
Phase 5  Go基礎 + TCP Proxy/LB           (Go)
Phase 6  自作コンテナランタイム            (Go)    ← Kubernetesへの架け橋
Phase 7  Kubernetes を使う                (k8s)
Phase 8  Kubernetes Controller / CRD 自作 (Go)
Phase 9  統合課題：RustKV Operator

【寄り道（興味に応じて）】
A. 非同期ランタイム自作（Future/Pin/Waker/epoll） (Rust)  … Phase 2の後
B. Memory Allocator → 自作OS                    (Rust)  … Phase 4の後
C. ユーザー空間TCPスタック（TUN/TAP）             (Rust)  … Phase 2の後
D. Raft / 分散KVS                               (Go)    … Phase 8の後
===================================================

| Rust | Go |
|---|---|
| HTTP Server | TCP Proxy |
| Protocol Parser | Load Balancer |
| Mini Redis | Distributed System |
| Storage Engine | Container Runtime |
| B+Tree / LSM | Kubernetes Controller |
| Memory Allocator | Kubernetes Operator |
| OS | Cloud Native |
| TCP Stack | Raftなど |

- Phase 0
https://doc.rust-jp.rs/book-ja
https://github.com/rust-lang/rustlings
https://google.github.io/comprehensive-rust/ja/

- Phase 1
作る順: TCP Echo → HTTP Request受信 → Parser → Response生成 → Router → Thread Pool
学べること: ソケット通信、バッファ操作（&[u8]）、マルチスレッド（Arc<Mutex<T>>）
教材マルチスレッドWebサーバーを構築する - ゼロからWebサーバーを作る最良のガイド
URL: https://doc.rust-jp.rs/book-ja/ch20-00-final-project-a-web-server.html

- Phase 2
作る順: RESPパーサー自作 → HashMapでGET/SET/DEL → 複数クライアント（Arc<Mutex<HashMap>>）→ Tokioでasync化 → EXPIRE/TTL
縛り: 外部のredis crateやserdeは使わず、&[u8]とenum/matchでパースする

Tokio公式チュートリアル  https://tokio.rs/tokio/tutorial
mini-redis（詰まったときに見る参考実装） https://github.com/tokio-rs/mini-redis
CodeCrafters「Build your own Redis」（一部有料）  https://codecrafters.io/
『詳解 Rustアトミック操作とロック』（英語版は無料）  https://marabos.nl/atomics/
『並行プログラミング入門』（高野祐輝／オライリー・ジャパン）
テスト駆動で段階的に進められる実践プラットフォーム: https://codecrafters.io/

- Phase 3
作るもの: 操作をWAL（Write-Ahead Logging）へ追記・fsyncする仕組み。Snapshotの作成と起動時のリストア
完了目安: データを書き込んだ直後に kill -9 でプロセスを強制終了しても、再起動後にデータが復元できること

教材: Mini-LSM の Durability 章 — WALの実装パターン
URL: https://skyzh.github.io/mini-lsm/
書籍: 『Database Internals』（Alex Petrov著 / O'Reilly） — WALとリカバリの理論的背景

3〜4 mini-lsm  https://skyzh.github.io/mini-lsm/
3〜4 『詳説 データベース』（『Database Internals』の邦訳）  書籍
3〜4 toydb（全体像の参照用）  https://github.com/erikgrinaker/toydb
3〜4 PingCAP Talent Plan https://github.com/pingcap/talent-plan

- Phase 4
LSMルート: MemTable → SSTable → Compaction → MVCC
B+Treeルート: 再帰構造、Box/Rc/RefCellと格闘しながらポインタと所有権の限界に挑む

教材・URL:
Mini-LSM — RustでLSM-Treeストレージエンジンを構築するステップ・バイ・ステップ教材
toydb — Rust製の学習用分散SQLデータベース: https://github.com/erikgrinaker/toydb

3〜4 mini-lsm  https://skyzh.github.io/mini-lsm/
3〜4 『詳説 データベース』（『Database Internals』の邦訳）  書籍
3〜4 toydb（全体像の参照用）  https://github.com/erikgrinaker/toydb
3〜4 PingCAP Talent Plan https://github.com/pingcap/talent-plan

- Phase 5
目的: Go言語の文法と固有の並行処理（goroutine, channel, context, netパッケージ）に慣れる
作るもの: TCP Proxy → Round Robin LB → ヘルスチェック → Circuit Breaker

A Tour of Go（日本語） https://go-tour-jp.appspot.com/
Effective Go  https://go.dev/doc/effective_go
build-your-own-x（題材探し）  https://github.com/codecrafters-io/build-your-own-x

- Phase 6
作るもの: mycontainer run /bin/sh（Linuxの隔離環境生成ツール）
学べること: Linux Namespace (PID, Mount, Net), cgroups, pivot_root, OCI仕様
注意: Linuxカーネルのsyscallを直接扱うため、Macの場合はLimaやMultipass等でLinux VM環境を用意すること

Liz Rice「Containers from Scratch」 https://github.com/lizrice/containers-from-scratch → Goでコンテナをゼロから作る講演動画およびコード
runc（Go製、読む用） https://github.com/opencontainers/runc
youki（Rust製、比較用）  https://github.com/youki-dev/youki → Rust製のOCIランタイム（コードリーディングの参考用）
OCI Runtime Spec  https://github.com/opencontainers/runtime-spec → コンテナランタイムの標準規格

- Phase 7
学習順: Pod → Deployment → Service → Ingress → ConfigMap/Secret → HPA → StatefulSet / PV
仕上げ: kind などのローカル環境で動かした後、Kubernetes the Hard Way で手動でコントロールプレーンを組み上げる

Kubernetes公式ドキュメント（日本語） https://kubernetes.io/ja/docs/
kind  https://kind.sigs.k8s.io/
『つくって、壊して、直して学ぶ Kubernetes入門』（翔泳社）  書籍
Kubernetes the Hard Way https://github.com/kelseyhightower/kubernetes-the-hard-way

- Phase 8
ゴール（集大成）: Phase 2〜3で作った Rust製 Mini Redis をプロビジョニング・運用する kind: RustKV のようなCRDとControllerをGoで作成する
学べること: Reconcile Loop, Desired State vs Actual State, Operatorパターン

Kubebuilder Book  https://book.kubebuilder.io/
sample-controller https://github.com/kubernetes/sample-controller

- Phase 9

- A. 非同期ランタイム自作 Phase 2の後 Tokio Tutorialの「Async in depth」、『並行プログラミング入門』
- B. Allocator → 自作OS Phase 4の後 https://os.phil-opp.com/ja/
- C. ユーザー空間TCPスタック  Phase 2の後 https://github.com/jonhoo/rust-tcp
- D. Raft / 分散KVS Phase 8の後 MIT 6.5840（Go）https://pdos.csail.mit.edu/6.824/

■業務で k8s/Go が急務になった場合の「迂回・並走ルート」
もし直近の業務で Kubernetes や Go の知識が必要になった場合は、メインルートを中断して以下の並走スタイルに切り替えることを強く推奨します。

【Go / k8s 優先ライン（平日・実務直結）】           【Rust 深層ライン（週末・長期投資）】
 1. A Tour of Go / Effective Go                  Phase 0  Rust基礎
 2. TCP Proxy / LB 自作 (Phase 5)                Phase 1  HTTP Server 自作
 3. Containers from Scratch (Phase 6)            Phase 2  Mini Redis 自作
 4. kind で k8s 基本操作 (Phase 7)                 Phase 3  WAL / 持続化
 5. Kubebuilder で Controller 自作 (Phase 8)       Phase 4  Storage Engine

「コンテナ自作 → k8s → Controller」の流れを先に終わらせることで、業務上のリターンを最短で得ることができる


②点目
学習計画へのコメント
全体としてとても良い計画

「小さく作って育てる」
「自分で仕様を決め、詰まったときだけ参考実装を見る」
「Rust で低レイヤー、Goでクラウドネイティブと役割を分ける」
という3本の柱がはっきりしている
題材の順序もおおむね理にかなっている

そのうえで、このまま進めると長すぎて、途中で止まりやすいという点が一番のリスク
以下、良い点、見直したい点、今のリポジトリとのつなげ方の順に記述

## 良い点
1. 1つのプロジェクトを育てる設計:
  Phase 1〜4 を rust-system-lab として同じリポジトリで育て、
  Phase 8〜9 で自分の Mini Redis を Operator で運用する流れは秀逸
  作ったものが次の段階の題材になるので、学んだことがつながり、やる気も保ちやすくなる
2. 完了条件がはっきりしている:
  Phase 3 の「kill -9 した後に再起動してもデータが戻る」のように、確かめられる形で書かれている
3. 縛りの付け方が適切:
  Phase 2 の「redis crate も serde も使わず、&[u8] と enum/match でパースする」は、所有権とバイト列の扱いを身に付けるのにちょうどよい制約
4. 寄り道を本筋から分けている:
  非同期ランタイム、OS、TCP スタック、Raft を寄り道として外に出しているので、深入りしても本筋に戻れる
5. 業務が急ぐ場合の並走ルートがある:現実的な保険になっている

## 見直したい点

### 1. Kubernetes に触れるまでが遠い(最大のリスク)
Phase 0〜4 がすべて Rust で、特に Phase 4(ストレージエンジン)は、それだけで数か月かかることもある題材
このままだと、k8s に触れるのが半年から1年先になりかねない

#### 提案:「業務が急ぐ場合」でなくても、最初から並走ルートを標準にすることをおすすめ

Rustライン:  Phase 0 → 1 → 2 → 3 ─────────────→ 4(任意の深さで)
Go/k8sライン:          Phase 6 → 7 → 8 ──→ 9(Phase 3 の成果物を使う)

Phase 8〜9 に必要なのは、Phase 3 までの「永続化できる Mini Redis」
Phase 4 が終わっていなくても Operator は作れるので、Phase 4 はほかの段階を止めない位置に置くのが合理的

### 2. Phase 5(Go の基礎)はもっと短くできる
このリポジトリで、backend、bff、admin/go、gateway/go を Go で読み書きしてきているので、A Tour of Go から始める必要はほぼない

#### 提案:Phase 5 は「TCP Proxy → Round Robin → ヘルスチェック → Circuit Breaker」の実装だけに絞る
context によるキャンセル、net.Conn と io.Copy、goroutine のリーク対策を意識的に学ぶ段階にすると、1〜2週間に収まる
gateway/go の実装を読んでから始めると、さらに早く進められる

### 3. Phase 4 の B+Tree は Rc/RefCell で作らない方がよい
計画には「Box、Rc、RefCell と格闘する」とありますが、これは実際のストレージエンジンの作り方ではない
消耗しやすい割に、得るものが少ない道

#### 提案:
- 実際の DB は、ノードをポインタでつなぐのではなく、ページ ID(整数)で参照する
  - ページは固定長で、ディスクとバッファプールの上にある
- 最初から「Vec<Page>(アリーナ)+ PageId で参照する」形で作ると、借用チェッカーとの無駄な格闘を避けられる
  - そのまま Phase 4 後半のページ管理とバッファプールにもつながる
- Rc/RefCell の練習がしたいなら、Phase 0 で双方向連結リストを作る程度で十分です(『Learning Rust With Entirely Too Many Linked Lists』が定番)
- どちらを先にやるか迷うなら、LSM(mini-lsm)を先にやるのがおすすめ
  - 段階ごとの教材とテストが揃っていて、Phase 3 の WAL とも自然につながる

### 4. Phase 9 が空欄になっている
集大成なので、中身を決めておくとゴールがはっきりします。案を書いておく

#### Phase 9:RustKV Operator(完成の条件)
- kind: RustKV の CR を作ると、StatefulSet、PVC、Service が作られる
- レプリカ数を変えると追従する(最初は単一ノードでよい)
- Pod を削除しても、PV の WAL と Snapshot からデータが戻る(Phase 3 の成果をここで確かめる)
- status に Ready の状態と接続先を返す
- 余裕があれば、バックアップ用の CRD(kind: RustKVBackup)と、Prometheus 形式のメトリクス

### 5. 環境についての注意(Apple Silicon の Mac)
- Phase 6:計画にあるとおり、Linux の VM が必要です。**Lima(arm64 の Ubuntu)**がおすすめ
  - 現在の Linux は cgroup v2 が標準なので、Liz Rice の講演のコード(cgroup v1 前提の部分がある)は、そのままでは動かない箇所がある
- 寄り道 B(自作 OS):phil-opp の教材は x86_64 向けなので、M シリーズの Mac では QEMU によるエミュレーションになる(遅いが動く)

## 今のリポジトリとつなげると学びが深まる
このリポジトリには、計画の題材とそのままつながるものがすでにある
┌───────────────────────────┬─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
│        計画の段階         │                                                                           このリポジトリで使えるもの                                                                            │
├───────────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Phase 1〜2(Rust の        │ backend-rust(Axum + tonic + tokio)。自作の HTTP サーバーを作った後に読むと、tower や hyper が何を肩代わりしているかがよく分かる                                                 │
│ HTTP、async)              │                                                                                                                                                                                 │
├───────────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Phase 5(TCP Proxy、LB)    │ gateway/go(Go 製のリバースプロキシ)と gateway/nginx を比べられる                                                                                                                │
├───────────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Phase 7(k8s を使う)       │ このリポジトリを kind に載せるのが、最高の練習題材になる。Deployment、Service、Ingress、ConfigMap/Secret(Keycloak                                                               │
│                           │ のクライアントシークレットや共有シークレット)、StatefulSet(MySQL、Redis)を一通り使う                                                                                            │
├───────────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Phase 7 の応用            │ docker-compose.yaml の冒頭のコメントにある「bff をコンテナにすると、Keycloak の issuer がブラウザ側とずれる問題」は、Ingress                                                    │
│                           │ でホスト名を揃えると解決できる。実務でもよくある問題なので、ここを解けると理解が一段深まる                                                                                      │
├───────────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Phase 8                   │ Feature Flag を CRD にする(kind: FeatureFlag → Controller が MySQL へ反映する)のも、小さく始められる Controller の題材になる                                                    │
└───────────────────────────┴─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

## 期間の目安と完了条件
週に10〜15時間を想定した、かなり大まかな目安
┌──────────────────────┬─────────────────────┬─────────────────────────────────────────────────────────────────────────────────────────────────┐
│        Phase         │        目安         │                                          完了条件(例)                                           │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 0 Rust の基礎        │ 3〜5週              │ rustlings を全問解く。借用エラーを自分で直せる                                                  │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 1 HTTP Server        │ 2〜3週              │ wrk などで負荷をかけてもスレッドプールが詰まらない。Keep-Alive に対応                           │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 2 Mini Redis         │ 4〜6週              │ redis-cli から GET、SET、DEL、EXPIRE ができる。Tokio 版と、スレッド版のベンチマークを比べられる │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 3 永続化             │ 2〜4週              │ kill -9 の後も戻る。WAL の末尾が壊れていても起動できる                                          │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 4 ストレージエンジン │ 6〜12週(深さによる) │ mini-lsm の各章のテストが通る                                                                   │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 5 Go の Proxy と LB  │ 1〜2週              │ バックエンドを1台止めても、振り分けが続く                                                       │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 6 コンテナ           │ 2〜3週              │ mycontainer run /bin/sh で PID、Mount、UTS が隔離され、cgroup でメモリを制限できる              │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 7 k8s                │ 3〜5週              │ このリポジトリのコア構成が kind で動く                                                          │
├──────────────────────┼─────────────────────┼─────────────────────────────────────────────────────────────────────────────────────────────────┤
│ 8〜9 Operator        │ 4〜6週              │ 上の Phase 9 の完成の条件を満たす                                                               │
└──────────────────────┴─────────────────────┴─────────────────────────────────────────────────────────────────────────────────────────────────┘
並走ルートにすると、3〜4か月目に k8s に触れ始め、半年前後で Operator までが現実的な見通し

## 最初の2週間でやること
1. rust-system-lab リポジトリを作り、README に各 Phase の完了条件を書いておく(後で振り返るとき、進み具合を確かめる基準になる)
2. The Book の4〜10章(所有権、構造体、enum、エラー処理、トレイト)と、rustlings の該当する問題を進める
3. 同時に、Lima で arm64 の Ubuntu の VM を用意しておく(Phase 6 と 7 の準備。先に用意しておくと、後で環境構築に詰まらずに済む)
4. 2週目の終わりに、Phase 1 の TCP Echo Server を std::net だけで作る

## まとめ:計画の中身はほぼこのままで大丈夫です。直すべきなのは主に次の3点
- 並走ルートを標準にする
- Phase 4 を、ほかの段階を止めない位置に置く
- B+Tree はページ ID 方式で作る

さらに、このリポジトリを kind に載せる課題を Phase 7 に入れると、手元にある題材を活かせる
```
