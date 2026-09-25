{-# OPTIONS_GHC -Wno-orphans #-}

-- | 生成された`Proto.Task.V1.Task`を再輸出し、grapesyがgRPCサービスとして扱うために
-- 必要な型family(メタデータ)を宣言する手書きモジュール。proto-lens-protocは
-- メッセージ/サービスの型しか生成しないため、grapesyとの接続部分はこのように
-- 手書きする(grapesyのtutorials/quickstart/src/Proto/API/Helloworld.hsと同じパターン)。
module Proto.API.Task.V1.Task
  ( module Proto.Task.V1.Task
  ) where

import Data.ProtoLens.Labels ()

import Network.GRPC.Common
import Network.GRPC.Common.Protobuf
import Network.GRPC.Spec (CustomMetadata)

import Proto.Task.V1.Task

-- | サーバー側はCall越しの低レベルAPI(getRequestHeaders)でauthorizationヘッダを直接読むため
-- 型付きメタデータの値そのものは使わないが、クライアント側は`CallParams`の
-- `callRequestMetadata`フィールド(型family`RequestMetadata`で決まる型)経由でしか送信メタデータを
-- 指定できない。`[CustomMetadata]`は`grpc-spec`があらかじめ`BuildMetadata`/`ParseMetadata`
-- インスタンスを用意している「生のヘッダリストそのまま」型なので、これを使うことで
-- クライアント側からauthorizationヘッダを送れるようにする(NoMetadataのままだと送信手段が無い)
type instance RequestMetadata (Protobuf TaskService meth) = [CustomMetadata]
type instance ResponseInitialMetadata (Protobuf TaskService meth) = NoMetadata
type instance ResponseTrailingMetadata (Protobuf TaskService meth) = NoMetadata
