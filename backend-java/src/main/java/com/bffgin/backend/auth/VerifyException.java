package com.bffgin.backend.auth;

public class VerifyException extends Exception {
    public VerifyException(String message) {
        super(message);
    }

    public VerifyException(String message, Throwable cause) {
        super(message, cause);
    }
}
