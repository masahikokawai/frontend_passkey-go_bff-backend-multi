//! backend-rust: CONTRACT.mdセクション20のTask CRUD Rust実装(Axum + tonic)。
//! lib.rsに切り出しているのは`tests/`配下の結合テストから内部モジュール
//! (db・auth・rest等)を直接使えるようにするため(Rustの`tests/`はlibクレート
//! でないと外部から内部モジュールを参照できない制約への対応)。main.rsはこの
//! lib crateを使う薄いバイナリになっている。

pub mod auth;
pub mod config;
pub mod db;
pub mod error;
pub mod external;
pub mod flags;
pub mod grpc;
pub mod logging;
pub mod model;
pub mod rest;

use sqlx::mysql::MySqlPool;

use auth::jwt::Dispatcher;

pub struct AppState {
    pub pool: MySqlPool,
    pub dispatcher: Dispatcher,
}
