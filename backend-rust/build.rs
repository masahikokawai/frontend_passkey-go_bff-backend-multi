fn main() -> Result<(), Box<dyn std::error::Error>> {
    // backend/proto/task/v1/task.proto をそのままコピーしたものを正本とする
    // (CONTRACT.mdセクション20.5: ワイヤー契約パリティのため、1文字も変えていない)
    // build_client(true): 動作確認用のgrpc smokeテスト(examples/grpc_smoke.rs)からも
    // 生成コードを使うため、クライアント側も生成する
    tonic_build::configure().build_server(true).build_client(true).compile_protos(
        &["proto/task/v1/task.proto"],
        &["proto"],
    )?;
    println!("cargo:rerun-if-changed=proto/task/v1/task.proto");
    Ok(())
}
