package com.bffgin.backend.test_support

/**
 * JUnit5のAssertions#assertThrowsはsuspend関数を受け取れない(Executableが非suspendのため)。
 * runTest{}の中からsuspend funの例外を検証するための小さなヘルパー
 */
suspend inline fun <reified T : Throwable> assertThrowsSuspend(block: suspend () -> Unit): T {
    try {
        block()
    } catch (e: Throwable) {
        if (e is T) return e
        throw AssertionError("Expected ${T::class.simpleName} but got ${e::class.simpleName}: ${e.message}", e)
    }
    throw AssertionError("Expected ${T::class.simpleName} to be thrown, but nothing was thrown")
}
