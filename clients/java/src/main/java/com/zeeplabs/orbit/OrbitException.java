package com.zeeplabs.orbit;

public class OrbitException extends RuntimeException {
    public final int status;

    public OrbitException(String message, int status) {
        super(message);
        this.status = status;
    }
}
