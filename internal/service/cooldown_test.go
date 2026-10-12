package service

import (
	"fmt"
	"testing"
	"time"

	"claude2api/internal/repository"
)

func TestParseRateLimitResetFromTruncatedBody(t *testing.T) {
	raw := `rate limit exceeded: {"type":"error","error":{"type":"rate_limit_error","message":"{\"type\":\"exceeded_limit\",\"resetsAt\":1791790200,\"remaining\":null,\"perModelLimit\":false,\"representativeClaim\":\"five_hour\""}}`
	now := time.Unix(1791790000, 0)
	got, ok := parseRateLimitReset(raw, now)
	want := time.Unix(1791790200, 0)
	if !ok || !got.Equal(want) {
		t.Fatalf("got %v ok=%v want %v", got, ok, want)
	}
}

func TestParseRateLimitResetMillisAndRFC3339(t *testing.T) {
	now := time.Unix(1791790000, 0)
	millis, ok := parseRateLimitReset(`{"resetsAt":1791790200000}`, now)
	if !ok || !millis.Equal(time.UnixMilli(1791790200000)) {
		t.Fatalf("millis: %v ok=%v", millis, ok)
	}
	iso, ok := parseRateLimitReset(`{"resetsAt":"2026-10-12T07:30:00Z"}`, now)
	want, _ := time.Parse(time.RFC3339, "2026-10-12T07:30:00Z")
	if !ok || !iso.Equal(want) {
		t.Fatalf("rfc3339: %v ok=%v", iso, ok)
	}
}

func TestRateLimitUntilFallsBackWhenResetMissingOrPast(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if _, ok := parseRateLimitReset("rate limit exceeded: not-json", now); ok {
		t.Fatal("invalid body was parsed")
	}
	if _, ok := parseRateLimitReset(`{"resetsAt":1000}`, now); ok {
		t.Fatal("past reset was accepted")
	}
	until := rateLimitUntil(fmt.Errorf("rate limit exceeded: not-json"), now)
	if until.Sub(now) != rateLimitFallbackCooldown {
		t.Fatalf("fallback %s", until.Sub(now))
	}
	far := now.Add(48 * time.Hour).Unix()
	clamped, ok := parseRateLimitReset(fmt.Sprintf(`{"resetsAt":%d}`, far), now)
	if !ok || !clamped.Equal(now.Add(rateLimitMaxCooldown)) {
		t.Fatalf("clamp %v ok=%v", clamped, ok)
	}
}

func TestAvailableAccountsSkipsCoolingAndExpired(t *testing.T) {
	accounts := []repository.Account{
		{Email: "limited@example.com", Status: "active", Cookies: map[string]string{"sessionKey": "a"}},
		{Email: "ready@example.com", Status: "active", Cookies: map[string]string{"sessionKey": "b"}},
		{Email: "expired@example.com", Status: "expired", Cookies: map[string]string{"sessionKey": "c"}},
		{Email: "empty@example.com", Status: "active"},
	}
	got := availableAccounts(accounts, map[string]bool{"limited@example.com": true}, map[string]bool{"ready@example.com": true})
	if len(got) != 0 {
		t.Fatalf("all accounts should be filtered, got %+v", got)
	}
	got = availableAccounts(accounts, map[string]bool{"limited@example.com": true}, nil)
	if len(got) != 1 || got[0].Email != "ready@example.com" {
		t.Fatalf("got %+v", got)
	}
}

func TestCoolAccountHidesAccountUntilReset(t *testing.T) {
	email := "limit@example.com"
	t.Cleanup(func() {
		apiIndexLock.Lock()
		delete(accountCooldown, email)
		apiIndexLock.Unlock()
	})
	until := time.Now().Add(time.Hour).Truncate(time.Second)
	coolAccount(email, until)
	account := repository.Account{Email: email, Status: "active", Cookies: map[string]string{"sessionKey": "x"}}

	apiIndexLock.Lock()
	cooling := pruneAndCoolingLocked(time.Now())
	apiIndexLock.Unlock()
	if got := availableAccounts([]repository.Account{account}, cooling, nil); len(got) != 0 {
		t.Fatalf("cooled account still selectable: %+v", got)
	}

	apiIndexLock.Lock()
	cooling = pruneAndCoolingLocked(until.Add(time.Second))
	apiIndexLock.Unlock()
	if _, still := cooling[email]; still {
		t.Fatal("expired cooldown was kept")
	}
	if got := availableAccounts([]repository.Account{account}, cooling, nil); len(got) != 1 {
		t.Fatalf("account should return after reset, got %+v", got)
	}
}
