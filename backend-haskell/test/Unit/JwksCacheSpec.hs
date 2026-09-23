module JwksCacheSpec (spec) where

import Control.Concurrent (forkIO)
import Control.Concurrent.MVar (newEmptyMVar, putMVar, takeMVar)
import Control.Monad (forM_, replicateM)
import Data.Maybe (isJust)
import qualified Data.Text as T
import Test.Hspec

import BackendHaskell.Auth.JwksCache (httpFetchViaClient, lookupKey, newJwksCache, refresh)
import MockJwksServer
import TestTokenHelper (generateRsaKeyPair, publicJwkFields)

spec :: Spec
spec = around withMockJwksServer $ describe "JwksCache" $ do
  it "lookup returns Nothing before any refresh" $ \(_mock, port) -> do
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    found <- lookupKey cache "kid-1"
    found `shouldBe` Nothing

  it "refresh populates the cache from the JWKS endpoint" $ \(mock, port) -> do
    (pub, _priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-1" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient
    result <- refresh cache
    result `shouldBe` Right ()
    found <- lookupKey cache "kid-1"
    found `shouldSatisfy` isJust

  -- 【STMの並行安全性を実際に確認する、この実装固有のテスト】
  -- 複数のスレッドが同時にrefresh(書き込み)とlookup(読み取り)を行っても、
  -- STMのトランザクションが競合を検出し自動的にリトライするため、キャッシュの状態が
  -- 破損したり、読み取り側が中途半端に更新された状態を見てクラッシュしたりしないことを確認する。
  -- ロック(Mutex)を明示的に取得・解放するコードが一切登場しない点が、Kotlin(Mutex)・
  -- Java(ConcurrentHashMap)・Elixir(GenServer)との対比のポイントである
  it "handles many concurrent refreshes and lookups without corruption" $ \(mock, port) -> do
    (pub, _priv) <- generateRsaKeyPair
    let (n, e) = publicJwkFields pub
    addKey mock "kid-concurrent" (n, e)
    cache <- newJwksCache (jwksUrlOf port) httpFetchViaClient

    let concurrency = 20 :: Int
    doneFlags <- replicateM concurrency newEmptyMVar
    forM_ doneFlags $ \done ->
      forkIO $ do
        _ <- refresh cache
        _ <- lookupKey cache "kid-concurrent"
        putMVar done ()
    mapM_ takeMVar doneFlags

    found <- lookupKey cache "kid-concurrent"
    found `shouldSatisfy` isJust

jwksUrlOf :: Int -> T.Text
jwksUrlOf port = T.pack ("http://127.0.0.1:" ++ show port ++ "/jwks")
