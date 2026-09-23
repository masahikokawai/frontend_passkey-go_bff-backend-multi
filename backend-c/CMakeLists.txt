cmake_minimum_required(VERSION 3.20)
# 【gRPC統合に伴う言語追加】このプロジェクト自身はCのみで書くが、gRPC Core(grpc/grpc.hという
# extern "C"のC ABIを公開してはいるものの、実装自体はC++で書かれている)のCMake config
# (gRPCConfig.cmake)がCXXコンパイラの機能検出を要求するため、project()にCXXも加える必要がある
# (「gRPCのC API」はCで書かれたライブラリという意味ではなく、Cから呼べるABIという意味)
project(backend_c C CXX)

set(CMAKE_C_STANDARD 11)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# AddressSanitizer/UndefinedBehaviorSanitizer: -DENABLE_ASAN=ON でこのプロジェクト自身の
# ターゲット(backend_c_lib/backend_c_server/backend_c_tests/backend_c_integration_tests)
# にのみ付与する。CivetWeb/Unity(FetchContentで取得する依存先)には付与しない
# (サニタイザ有無が混在したコード同士のリンク自体は標準的にサポートされている。
# 過去にCIVETWEB_ENABLE_ASANが既定ONで自プロジェクト側と食い違いリンクエラーになった経緯が
# あるため、CivetWeb側は下記の通り常にOFFに固定している)
option(ENABLE_ASAN "Build with AddressSanitizer + UndefinedBehaviorSanitizer" OFF)

# backend-cppと同じくHomebrewを採用する(vcpkgを使わない経緯はbackend-cpp/README.md参照)
list(APPEND CMAKE_PREFIX_PATH /opt/homebrew)
list(APPEND CMAKE_PREFIX_PATH /opt/homebrew/opt/grpc /opt/homebrew/opt/protobuf)
# JWT/JWKS本実装(src/auth/)がOpenSSLのプリミティブを直接使う(backend-cppと同じくopenssl@3)
list(APPEND CMAKE_PREFIX_PATH /opt/homebrew/opt/openssl@3)

find_package(Threads REQUIRED)
find_package(cJSON CONFIG REQUIRED)
# backend-cppと同じくCONFIG modeを明示する(レガシーFindProtobuf.cmakeとの混在エラー回避)
find_package(Protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)
find_package(OpenSSL REQUIRED)
# JWKS取得(src/auth/jwks_verifier.c)用。macOSのXcode SDKに同梱されているシステムlibcurlを
# そのまま使う(Homebrewにインストールする必要は無い、curl-config/ヘッダともSDKに含まれる)
find_package(CURL REQUIRED)

# protobuf-cはCMake configを提供しないため、libmysqlclientと同じfind_path/find_libraryで探す
find_path(PROTOBUF_C_INCLUDE_DIR protobuf-c/protobuf-c.h
  PATHS /opt/homebrew/opt/protobuf-c/include /opt/homebrew/Cellar/protobuf-c/*/include)
find_library(PROTOBUF_C_LIBRARY
  NAMES protobuf-c
  PATHS /opt/homebrew/opt/protobuf-c/lib /opt/homebrew/Cellar/protobuf-c/*/lib)
find_program(PROTOC_GEN_C_EXECUTABLE protoc-gen-c
  PATHS /opt/homebrew/bin)

if(NOT PROTOBUF_C_INCLUDE_DIR OR NOT PROTOBUF_C_LIBRARY OR NOT PROTOC_GEN_C_EXECUTABLE)
  message(FATAL_ERROR "protobuf-c not found: brew install protobuf-c")
endif()

# --- proto/task/v1/task.proto からC言語コードを生成する(gRPC v2、README.md「gRPC」節参照) ---
#
# 【protobuf-cの既知の制約】protoc-gen-c(1.5.2時点)はproto3のoptionalフィールドに
# 対応していない(生成時に "hasn't been updated to support optional fields in proto3"
# エラーで失敗する)。正本のproto/task/v1/task.protoは他言語と1文字も違わない
# (optional string description含む、CONTRACT.mdセクション20.5)ため書き換えず、
# ビルド時にoptionalだけを取り除いたコピーを一時生成し、それだけをprotoc-gen-cへ渡す
# (README.md「protobuf-cの制約」節参照)。
# 【この回避策で失われる意味論】optional無しの通常のproto3フィールドは「値が空文字列」と
# 「フィールド自体が未設定」を区別できない(どちらもワイヤー上で送信されない)。
# 他言語はoptionalにより明示的な空文字列送信を区別できるが、本プロジェクトのdescriptionは
# そのケースを実際には使わないため実害は無いと判断している
set(PROTO_GEN_DIR "${CMAKE_CURRENT_BINARY_DIR}/generated")
set(PROTO_GEN_SUBDIR "${PROTO_GEN_DIR}/task/v1")
file(MAKE_DIRECTORY "${PROTO_GEN_SUBDIR}")
set(PROTO_FILE "${CMAKE_CURRENT_SOURCE_DIR}/proto/task/v1/task.proto")

set(PROTO_C_INPUT_DIR "${CMAKE_CURRENT_BINARY_DIR}/proto_c_input")
set(PROTO_C_FILE "${PROTO_C_INPUT_DIR}/task/v1/task.proto")
file(MAKE_DIRECTORY "${PROTO_C_INPUT_DIR}/task/v1")

add_custom_command(
  OUTPUT "${PROTO_C_FILE}"
  COMMAND /bin/sh -c "sed -e 's/optional string description/string description/' '${PROTO_FILE}' > '${PROTO_C_FILE}'"
  DEPENDS "${PROTO_FILE}"
  COMMENT "Stripping proto3 optional into build-only copy (protoc-gen-c limitation)"
  VERBATIM)

find_path(PROTOBUF_WELL_KNOWN_TYPES_DIR google/protobuf/timestamp.proto
  PATHS /opt/homebrew/include)

set(TASK_PROTO_C_SRC "${PROTO_GEN_SUBDIR}/task.pb-c.c")
set(TASK_PROTO_C_HDR "${PROTO_GEN_SUBDIR}/task.pb-c.h")
add_custom_command(
  OUTPUT "${TASK_PROTO_C_SRC}" "${TASK_PROTO_C_HDR}"
  COMMAND protobuf::protoc
    "--plugin=protoc-gen-c=${PROTOC_GEN_C_EXECUTABLE}"
    "--c_out=${PROTO_GEN_DIR}"
    -I "${PROTO_C_INPUT_DIR}"
    -I "${PROTOBUF_WELL_KNOWN_TYPES_DIR}"
    "${PROTO_C_FILE}"
  DEPENDS "${PROTO_C_FILE}" "${PROTOC_GEN_C_EXECUTABLE}"
  COMMENT "Generating C code from task.proto (protobuf-c)"
  VERBATIM)

file(MAKE_DIRECTORY "${PROTO_GEN_DIR}/google/protobuf")
set(TIMESTAMP_PROTO_FILE "${PROTOBUF_WELL_KNOWN_TYPES_DIR}/google/protobuf/timestamp.proto")
set(TIMESTAMP_PROTO_C_SRC "${PROTO_GEN_DIR}/google/protobuf/timestamp.pb-c.c")
set(TIMESTAMP_PROTO_C_HDR "${PROTO_GEN_DIR}/google/protobuf/timestamp.pb-c.h")
add_custom_command(
  OUTPUT "${TIMESTAMP_PROTO_C_SRC}" "${TIMESTAMP_PROTO_C_HDR}"
  COMMAND protobuf::protoc
    "--plugin=protoc-gen-c=${PROTOC_GEN_C_EXECUTABLE}"
    "--c_out=${PROTO_GEN_DIR}"
    -I "${PROTOBUF_WELL_KNOWN_TYPES_DIR}"
    "${TIMESTAMP_PROTO_FILE}"
  DEPENDS "${TIMESTAMP_PROTO_FILE}" "${PROTOC_GEN_C_EXECUTABLE}"
  COMMENT "Generating C code from google/protobuf/timestamp.proto (protobuf-c)"
  VERBATIM)

add_library(task_proto_c STATIC ${TASK_PROTO_C_SRC} ${TIMESTAMP_PROTO_C_SRC})
target_include_directories(task_proto_c PUBLIC "${PROTO_GEN_DIR}" ${PROTOBUF_C_INCLUDE_DIR})
target_link_libraries(task_proto_c PUBLIC ${PROTOBUF_C_LIBRARY})

# mysql-connector-c++と違いlibmysqlclientはCMake configを提供しないため、
# backend-cpp/CMakeLists.txtと同じfind_path/find_libraryパターンで探す
find_path(MYSQLCLIENT_INCLUDE_DIR mysql/mysql.h
  PATHS /opt/homebrew/opt/mysql-client/include /opt/homebrew/Cellar/mysql-client/*/include)
find_library(MYSQLCLIENT_LIBRARY
  NAMES mysqlclient
  PATHS /opt/homebrew/opt/mysql-client/lib /opt/homebrew/Cellar/mysql-client/*/lib)

if(NOT MYSQLCLIENT_INCLUDE_DIR OR NOT MYSQLCLIENT_LIBRARY)
  message(FATAL_ERROR "libmysqlclient not found: brew install mysql-client")
endif()

# --- CivetWeb(HTTPサーバー): HomebrewにformulaがないためFetchContentで取得する ---
# TLS/C++/テスト/サンプル実行ファイルはこのフェーズでは不要なため全て無効化する
include(FetchContent)
set(CIVETWEB_ENABLE_SSL OFF CACHE BOOL "" FORCE)
set(CIVETWEB_ENABLE_CXX OFF CACHE BOOL "" FORCE)
set(CIVETWEB_BUILD_TESTING OFF CACHE BOOL "" FORCE)
set(CIVETWEB_ENABLE_SERVER_EXECUTABLE OFF CACHE BOOL "" FORCE)
# CIVETWEB_ENABLE_ASANはDebugビルドで既定ONになり-fsanitize=undefinedを付与するが、
# 自分のターゲット(backend_c_server等)側はサニタイザ付きでリンクしないためシンボル未解決に
# なる。CMAKE_BUILD_TYPE=Debugでも自プロジェクトはサニタイザ無しでビルドしたいため無効化する
set(CIVETWEB_ENABLE_ASAN OFF CACHE BOOL "" FORCE)

# civetweb v1.16のCMakeLists.txtはcmake_minimum_required(VERSION 2.8...)相当で古く、
# 手元のCMakeでは`cmake_minimum_required(VERSION < 3.5)`が拒否されるため、
# civetweb自体のCMakeポリシーバージョンだけ下限を明示的に許可する
set(CMAKE_POLICY_VERSION_MINIMUM 3.5)

FetchContent_Declare(
  civetweb
  GIT_REPOSITORY https://github.com/civetweb/civetweb.git
  GIT_TAG v1.16
)
FetchContent_MakeAvailable(civetweb)

unset(CMAKE_POLICY_VERSION_MINIMUM)

# --- Unity(Cテストフレームワーク): Homebrewにformulaがないためこちらも同様に取得する ---
set(UNITY_EXTENSION_FIXTURE ON CACHE BOOL "" FORCE)
FetchContent_Declare(
  unity
  GIT_REPOSITORY https://github.com/ThrowTheSwitch/Unity.git
  GIT_TAG v2.6.0
)
FetchContent_MakeAvailable(unity)

add_library(backend_c_lib
  src/domain/task.c
  src/common/error.c
  src/common/log.c
  src/common/time.c
  src/db/mysql_conn.c
  src/repository/task_repository.c
  src/application/task_validation.c
  src/auth/base64url.c
  src/auth/jwt.c
  src/auth/dispatcher.c
  src/auth/hmac_verifier.c
  src/auth/jwks_verifier.c
  src/auth/user_resolver.c
  src/auth/auth_module.c
  src/auth/external_auth.c
  src/http/task_json.c
  src/http/handler.c
  src/flags/feature_flag_poller.c
  src/external/external_query.c
  src/external/external_handler.c
  src/grpc/task_mapper.c
  src/grpc/task_grpc_handlers.c
  src/grpc/grpc_server.c
)
target_include_directories(backend_c_lib PUBLIC src ${MYSQLCLIENT_INCLUDE_DIR})
target_link_libraries(backend_c_lib PUBLIC
  Threads::Threads
  cjson
  ${MYSQLCLIENT_LIBRARY}
  civetweb-c-library
  task_proto_c
  gRPC::grpc
  OpenSSL::Crypto
  CURL::libcurl
)
if(ENABLE_ASAN)
  target_compile_options(backend_c_lib PUBLIC -fsanitize=address,undefined -fno-omit-frame-pointer -g)
  target_link_options(backend_c_lib PUBLIC -fsanitize=address,undefined)
endif()

add_executable(backend_c_server src/main.c)
target_link_libraries(backend_c_server PRIVATE backend_c_lib)

enable_testing()
add_executable(backend_c_tests
  tests/task_validation_test.c
)
target_link_libraries(backend_c_tests PRIVATE backend_c_lib unity)
add_test(NAME backend_c_tests COMMAND backend_c_tests)

# JWT/JWKS本実装の単体テスト。DB・Keycloak不要(backend-rust/src/auth/jwt.rsのテスト
# モジュールと同じ「HMAC経路+Dispatcherのみ純粋単体テスト」の方針、README.md「認証について」参照)
add_executable(backend_c_jwt_tests
  tests/jwt_test.c
  tests/test_token_helper.c
)
target_link_libraries(backend_c_jwt_tests PRIVATE backend_c_lib unity)
add_test(NAME backend_c_jwt_tests COMMAND backend_c_jwt_tests)

# JWKS(RS256)経路の単体テスト。CivetWebで自プロセス内にモックJWKSサーバーを立てるため
# 実Keycloak・実DBは不要(backend-rust/backend-cppには無いテスト、README.md参照)
add_executable(backend_c_jwks_tests
  tests/jwks_test.c
  tests/test_token_helper.c
)
target_link_libraries(backend_c_jwks_tests PRIVATE backend_c_lib unity)
add_test(NAME backend_c_jwks_tests COMMAND backend_c_jwks_tests)

# 外部公開API(src/external/external_query.c)のクエリパース処理の単体テスト。
# 実サーバー・DB不要(文字列を渡すだけの純粋関数)
add_executable(backend_c_external_query_tests
  tests/external_query_test.c
)
target_link_libraries(backend_c_external_query_tests PRIVATE backend_c_lib unity)
add_test(NAME backend_c_external_query_tests COMMAND backend_c_external_query_tests)

# 実DBに接続する結合テスト(tests/task_repository_integration_test.c参照)。
# ctestのデフォルト対象には含めない(docker-compose上のMySQLが必須なため、
# add_test()には登録せず手動実行のみとする。実行方法はREADME.md「結合テスト」節を参照)
add_executable(backend_c_integration_tests
  tests/task_repository_integration_test.c
)
target_link_libraries(backend_c_integration_tests PRIVATE backend_c_lib unity)

# gRPCの結合テスト(tests/task_grpc_integration_test.c参照)。実際に:9100相当のポートで
# grpc_server_module_startしたサーバーへ、grpc-core C APIで書いた自前クライアント
# (tests/grpc_test_client.c)からRPCを送る。実DB接続が必須なためctestには登録しない
add_executable(backend_c_grpc_integration_tests
  tests/task_grpc_integration_test.c
  tests/grpc_test_client.c
  tests/test_token_helper.c
)
target_link_libraries(backend_c_grpc_integration_tests PRIVATE backend_c_lib unity)

# Feature Flagポーリング(src/flags/feature_flag_poller.c)の結合テスト。実DB必須のため
# ctestには登録しない(tests/feature_flag_poller_test.c参照)
add_executable(backend_c_feature_flag_poller_tests
  tests/feature_flag_poller_test.c
)
target_link_libraries(backend_c_feature_flag_poller_tests PRIVATE backend_c_lib unity)

# 外部公開API(src/external/external_handler.c)の結合テスト。実DB+モックJWKSサーバー+
# 外部公開API自身のCivetWebリスナーを全て自プロセス内で起動する
# (tests/external_handler_integration_test.c参照)。実DB接続が必須なためctestには登録しない
add_executable(backend_c_external_integration_tests
  tests/external_handler_integration_test.c
  tests/test_token_helper.c
)
target_link_libraries(backend_c_external_integration_tests PRIVATE backend_c_lib unity)
