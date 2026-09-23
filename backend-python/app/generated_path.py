"""app/generatedディレクトリをsys.pathへ追加する。protoc生成コード(task_pb2_grpc.py)は
`from task.v1 import task_pb2`という、task/をトップレベルパッケージとして扱うimportを
生成する(grpc_tools.protocの既定の挙動、生成コード自体は変更しない方針のため)ため、
app/generatedそのものをsys.pathへ含める必要がある。gRPC関連のモジュールをimportする前に、
必ずこのモジュールを先にimportすること(app.grpc_パッケージの__init__.pyとconftest.pyの
両方から呼ばれる)
"""
from __future__ import annotations

import os
import sys

_GENERATED_DIR = os.path.join(os.path.dirname(__file__), "generated")
if _GENERATED_DIR not in sys.path:
    sys.path.insert(0, _GENERATED_DIR)
