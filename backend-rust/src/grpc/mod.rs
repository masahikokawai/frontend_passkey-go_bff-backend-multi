pub mod task;

pub mod pb {
    // backend/proto/task/v1/task.proto をそのままコピーしたものからtonic-buildが生成する
    // (build.rs参照。CONTRACT.mdセクション20.5: ワイヤー契約パリティ)
    tonic::include_proto!("task.v1");
}
