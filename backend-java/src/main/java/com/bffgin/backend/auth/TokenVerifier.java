package com.bffgin.backend.auth;

public interface TokenVerifier {
    Claims verify(String token) throws VerifyException;
}
