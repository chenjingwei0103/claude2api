package service

import (
	"strconv"
	"strings"
	"time"

	"claude2api/internal/repository"
)

const (
	rateLimitFallbackCooldown = 45 * time.Second
	rateLimitMaxCooldown      = 24 * time.Hour
)

// accountCooldown 记录账号限额恢复时间。只保存在内存中，重启后清空。
var accountCooldown = map[string]time.Time{}

// coolAccount 把账号冷却到 until。更晚的恢复时间会覆盖更早的。
func coolAccount(email string, until time.Time) {
	if email == "" || !until.After(time.Now()) {
		return
	}
	apiIndexLock.Lock()
	defer apiIndexLock.Unlock()
	if prev, ok := accountCooldown[email]; !ok || until.After(prev) {
		accountCooldown[email] = until
	}
}

func pruneAndCoolingLocked(now time.Time) map[string]bool {
	cooling := make(map[string]bool, len(accountCooldown))
	for email, until := range accountCooldown {
		if until.After(now) {
			cooling[email] = true
			continue
		}
		delete(accountCooldown, email)
	}
	return cooling
}

func usableAccountsAllCooling() bool {
	accounts := repository.LoadAccounts()
	apiIndexLock.Lock()
	defer apiIndexLock.Unlock()
	cooling := pruneAndCoolingLocked(time.Now())
	usable := 0
	cooled := 0
	for i := range accounts {
		if !AccountUsable(&accounts[i]) {
			continue
		}
		usable++
		if cooling[accounts[i].Email] {
			cooled++
		}
	}
	return usable > 0 && usable == cooled
}

// availableAccounts 去掉不可用、冷却中，以及本次请求已经限额过的账号。
func availableAccounts(accounts []repository.Account, cooling map[string]bool, skip map[string]bool) []repository.Account {
	out := make([]repository.Account, 0, len(accounts))
	for i := range accounts {
		email := accounts[i].Email
		if !AccountUsable(&accounts[i]) || cooling[email] || skip[email] {
			continue
		}
		out = append(out, accounts[i])
	}
	return out
}

// rateLimitUntil 从 429 正文里取 resetsAt。解析失败时只用短冷却，避免把账号锁死。
func rateLimitUntil(err error, now time.Time) time.Time {
	if err != nil {
		if until, ok := parseRateLimitReset(err.Error(), now); ok {
			return until
		}
	}
	return now.Add(rateLimitFallbackCooldown)
}

func parseRateLimitReset(raw string, now time.Time) (time.Time, bool) {
	text := strings.ReplaceAll(raw, `\"`, `"`)
	key := `"resetsAt"`
	idx := strings.Index(text, key)
	if idx < 0 {
		idx = strings.Index(strings.ToLower(text), strings.ToLower(key))
		if idx < 0 {
			return time.Time{}, false
		}
	}
	rest := strings.TrimSpace(text[idx+len(key):])
	rest = strings.TrimLeft(rest, " \t\r\n:")
	if rest == "" {
		return time.Time{}, false
	}
	if rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return time.Time{}, false
		}
		return clampRateLimitReset(parseResetValue(rest[1:1+end]), now)
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return time.Time{}, false
	}
	return clampRateLimitReset(parseResetValue(rest[:end]), now)
}

func parseResetValue(value string) time.Time {
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n > 1_000_000_000_000 {
			return time.UnixMilli(n)
		}
		return time.Unix(n, 0)
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Time{}
}

func clampRateLimitReset(reset time.Time, now time.Time) (time.Time, bool) {
	if reset.IsZero() || !reset.After(now) {
		return time.Time{}, false
	}
	if reset.Sub(now) > rateLimitMaxCooldown {
		return now.Add(rateLimitMaxCooldown), true
	}
	return reset, true
}
