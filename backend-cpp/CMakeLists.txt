cmake_minimum_required(VERSION 3.20)
project(backend_cpp CXX)

set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# AddressSanitizer/UndefinedBehaviorSanitizer: -DENABLE_ASAN=ON でbackend_cpp_lib
# (すなわちbackend_cpp_server・全テストバイナリ)にのみ付与する。FetchContentする
# サードパーティ依存は現状無いため、backend-cのCivetWebのような「サニタイザ有無の
# 食い違いを避けるための個別OFF指定」は不要
option(ENABLE_ASAN "Build with AddressSanitizer + UndefinedBehaviorSanitizer" OFF)

# Homebrewの依存関係を見つける(vcpkgではなくHomebrewを採用した経緯はREADME.md参照)
# mysql-connector-c++はX DevAPI(X Protocol、既定ポート33060)のみを提供するが、
# docker-compose.yamlはclassic protocol(:3306→13306)しか公開していないため接続不可能
# (README.md「アーキテクチャ選定」節参照)。libmysqlclient(classic C API)を採用する
list(APPEND CMAKE_PREFIX_PATH /opt/homebrew)

list(APPEND CMAKE_PREFIX_PATH /opt/homebrew/opt/openssl@3)
list(APPEND CMAKE_PREFIX_PATH /opt/homebrew/opt/grpc /opt/homebrew/opt/protobuf)

find_package(Boost REQUIRED COMPONENTS system)
find_package(nlohmann_json REQUIRED)
find_package(Threads REQUIRED)
find_package(OpenSSL REQUIRED)
find_package(GTest QUIET)
# CONFIG modeを明示しないと、レガシーなFindProtobuf.cmake(Module mode)とgRPCが内部で使う
# protobuf-config.cmake(Config mode)が混在し、"targets already defined"エラーになる
find_package(Protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)

# --- proto/task/v1/task.proto から C++コードを生成する(gRPC v2、README.md「gRPC」節参照) ---
# 生成物はビルドディレクトリ配下(generated/)に置き、.gitignore対象にする
set(PROTO_GEN_DIR "${CMAKE_CURRENT_BINARY_DIR}/generated")
# protocは-Iの相対パス(task/v1/task.proto)をそのまま出力先に反映するため、
# 生成物は${PROTO_GEN_DIR}/task/v1/配下になる
set(PROTO_GEN_SUBDIR "${PROTO_GEN_DIR}/task/v1")
file(MAKE_DIRECTORY "${PROTO_GEN_SUBDIR}")
set(PROTO_FILE "${CMAKE_CURRENT_SOURCE_DIR}/proto/task/v1/task.proto")
set(PROTO_SRCS "${PROTO_GEN_SUBDIR}/task.pb.cc")
set(PROTO_HDRS "${PROTO_GEN_SUBDIR}/task.pb.h")
set(GRPC_SRCS "${PROTO_GEN_SUBDIR}/task.grpc.pb.cc")
set(GRPC_HDRS "${PROTO_GEN_SUBDIR}/task.grpc.pb.h")

# get_target_property(...LOCATION)は非推奨で信頼できないため、
# $<TARGET_FILE:...>ジェネレータ式で実際の実行ファイルパスを解決する
add_custom_command(
  OUTPUT "${PROTO_SRCS}" "${PROTO_HDRS}" "${GRPC_SRCS}" "${GRPC_HDRS}"
  COMMAND protobuf::protoc
    "--cpp_out=${PROTO_GEN_DIR}"
    "--grpc_out=${PROTO_GEN_DIR}"
    "--plugin=protoc-gen-grpc=$<TARGET_FILE:gRPC::grpc_cpp_plugin>"
    -I "${CMAKE_CURRENT_SOURCE_DIR}/proto"
    "${PROTO_FILE}"
  DEPENDS "${PROTO_FILE}" gRPC::grpc_cpp_plugin
  COMMENT "Generating C++ code from task.proto"
  VERBATIM)

add_library(task_proto STATIC ${PROTO_SRCS} ${GRPC_SRCS})
# task/v1/を直接includeパスに加えることで、他ソースからは"task.pb.h"のまま参照できるようにする
target_include_directories(task_proto PUBLIC "${PROTO_GEN_DIR}" "${PROTO_GEN_SUBDIR}")
target_link_libraries(task_proto PUBLIC protobuf::libprotobuf gRPC::grpc++)

find_path(MYSQLCLIENT_INCLUDE_DIR mysql/mysql.h
  PATHS /opt/homebrew/opt/mysql-client/include /opt/homebrew/Cellar/mysql-client/*/include)
find_library(MYSQLCLIENT_LIBRARY
  NAMES mysqlclient
  PATHS /opt/homebrew/opt/mysql-client/lib /opt/homebrew/Cellar/mysql-client/*/lib)

if(NOT MYSQLCLIENT_INCLUDE_DIR OR NOT MYSQLCLIENT_LIBRARY)
  message(FATAL_ERROR "libmysqlclient not found: brew install mysql-client")
endif()

add_library(backend_cpp_lib
  src/config.cpp
  src/domain/task.cpp
  src/common/error.cpp
  src/common/time.cpp
  src/common/logging.cpp
  src/db/connection_pool.cpp
  src/repository/task_repository.cpp
  src/http/session.cpp
  src/http/router.cpp
  src/application/task_handler.cpp
  src/application/user_resolver.cpp
  src/auth/jwt.cpp
  src/flags/feature_flag_poller.cpp
  src/external/external_handler.cpp
  src/grpc/task_mapper.cpp
  src/grpc/task_grpc_service.cpp
)

target_include_directories(backend_cpp_lib PUBLIC src ${MYSQLCLIENT_INCLUDE_DIR})
target_link_libraries(backend_cpp_lib PUBLIC
  Boost::system
  Threads::Threads
  nlohmann_json::nlohmann_json
  OpenSSL::Crypto
  ${MYSQLCLIENT_LIBRARY}
  task_proto
)
target_compile_definitions(backend_cpp_lib PUBLIC BOOST_ASIO_HAS_CO_AWAIT)

if(ENABLE_ASAN)
  target_compile_options(backend_cpp_lib PUBLIC -fsanitize=address,undefined -fno-omit-frame-pointer -g)
  target_link_options(backend_cpp_lib PUBLIC -fsanitize=address,undefined)
endif()

add_executable(backend_cpp_server src/main.cpp)
target_link_libraries(backend_cpp_server PRIVATE backend_cpp_lib)

if(GTest_FOUND)
  enable_testing()

  add_executable(backend_cpp_tests
    tests/model_test.cpp
  )
  target_link_libraries(backend_cpp_tests PRIVATE backend_cpp_lib GTest::gtest GTest::gtest_main)
  add_test(NAME backend_cpp_tests COMMAND backend_cpp_tests)

  # JWT/JWKS認証の単体テスト(実DB不要、ctestのデフォルト対象)。jwks_testはJWKSモック
  # サーバー(tests/mock_jwks_server.cpp)を自プロセス内に立てるが、実Keycloak/実DBには
  # 依存しないためこちら側に含める(backend-c/CMakeLists.txtの分類と同じ考え方)
  add_executable(backend_cpp_jwt_tests
    tests/jwt_test.cpp
    tests/test_token_helper.cpp
  )
  target_link_libraries(backend_cpp_jwt_tests PRIVATE backend_cpp_lib GTest::gtest GTest::gtest_main)
  add_test(NAME backend_cpp_jwt_tests COMMAND backend_cpp_jwt_tests)

  add_executable(backend_cpp_jwks_tests
    tests/jwks_test.cpp
    tests/test_token_helper.cpp
    tests/mock_jwks_server.cpp
  )
  target_link_libraries(backend_cpp_jwks_tests PRIVATE backend_cpp_lib GTest::gtest GTest::gtest_main)
  add_test(NAME backend_cpp_jwks_tests COMMAND backend_cpp_jwks_tests)

  # 外部公開API(CONTRACT.mdセクション11)のクエリパース単体テスト(実DB・実サーバー不要、
  # 文字列を渡すだけの純粋関数。backend-c/tests/external_query_test.cと同じ観点)
  add_executable(backend_cpp_external_query_tests
    tests/external_query_test.cpp
  )
  target_link_libraries(backend_cpp_external_query_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)
  add_test(NAME backend_cpp_external_query_tests COMMAND backend_cpp_external_query_tests)

  # 外部公開APIのRequireExternalClientAuth単体テスト(実DB不要。jwks_testsと同じくJWKS
  # モックサーバーを自プロセス内に立てる)
  add_executable(backend_cpp_external_auth_tests
    tests/external_auth_test.cpp
    tests/test_token_helper.cpp
    tests/mock_jwks_server.cpp
  )
  target_link_libraries(backend_cpp_external_auth_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)
  add_test(NAME backend_cpp_external_auth_tests COMMAND backend_cpp_external_auth_tests)

  # REST層のエラーマッピング(src/common/error.cpp)単体テスト(実DB・実サーバー不要、
  # AppErrorを渡すだけの純粋関数)
  add_executable(backend_cpp_rest_error_mapper_tests
    tests/rest_error_mapper_test.cpp
  )
  target_link_libraries(backend_cpp_rest_error_mapper_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)
  add_test(NAME backend_cpp_rest_error_mapper_tests COMMAND backend_cpp_rest_error_mapper_tests)

  # 実DB(docker-compose上のMySQL)に接続する結合テスト群。ctestのデフォルト対象には
  # 含めない(add_test()に登録しない、README.md「結合テスト」節参照、手動実行のみ)
  add_executable(backend_cpp_repository_integration_tests
    tests/task_repository_integration_test.cpp
    tests/db_fixture.cpp
  )
  target_link_libraries(backend_cpp_repository_integration_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)

  add_executable(backend_cpp_grpc_integration_tests
    tests/grpc_integration_test.cpp
    tests/db_fixture.cpp
    tests/test_token_helper.cpp
  )
  target_link_libraries(backend_cpp_grpc_integration_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)

  add_executable(backend_cpp_external_integration_tests
    tests/external_handler_integration_test.cpp
    tests/db_fixture.cpp
    tests/test_token_helper.cpp
    tests/mock_jwks_server.cpp
    tests/http_test_client.cpp
  )
  target_link_libraries(backend_cpp_external_integration_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)

  add_executable(backend_cpp_feature_flag_poller_tests
    tests/feature_flag_poller_test.cpp
  )
  target_link_libraries(backend_cpp_feature_flag_poller_tests PRIVATE
    backend_cpp_lib GTest::gtest GTest::gtest_main)
endif()
