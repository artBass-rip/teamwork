package main

import "testing"

func TestPKCEChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	if got := challenge(verifier); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("unexpected challenge: %s", got)
	}
}

func TestLoopbackRedirectMatchesRegisteredDesktopURI(t *testing.T) {
	if got := loopbackRedirect(49152); got != "http://localhost:49152" {
		t.Fatalf("unexpected loopback redirect: %s", got)
	}
}
