package middleware

import (
	"net/http"
	"nora/util"
	"strings"
	"sync"
	"time"
)

// visitor sleduje stav token bucket pro jednu IP adresu.
type visitor struct {
	tokens   float64
	lastSeen time.Time
}

// RateLimiter — jednoduchý token bucket per IP.
// Zachován pro zpětnou kompatibilitu (testy).
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     float64
	burst    int
}

func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		burst:    burst,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastSeen) > 3*time.Minute {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	now := time.Now()

	if !exists {
		rl.visitors[ip] = &visitor{tokens: float64(rl.burst) - 1, lastSeen: now}
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.tokens += elapsed * rl.rate
	if v.tokens > float64(rl.burst) {
		v.tokens = float64(rl.burst)
	}
	v.lastSeen = now

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := util.GetClientIP(r)

		if !rl.allow(ip) {
			util.Error(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// --- Per-endpoint rate limiting ---

// Tier definuje úroveň rate limitingu.
type Tier int

const (
	TierStrict  Tier = iota // auth endpointy — 3 req/s, burst 5
	TierNormal              // CRUD operace — default (z configu nebo 10/20)
	TierRelaxed             // čtení, listing — 30 req/s, burst 50
	TierUpload              // upload endpointy — 5 req/s, burst 10
)

// tierConfig definuje rate a burst pro daný tier.
type tierConfig struct {
	Rate  float64
	Burst int
}

// Výchozí konfigurace tierů.
var defaultTierConfigs = map[Tier]tierConfig{
	TierStrict:  {Rate: 3, Burst: 5},
	TierNormal:  {Rate: 10, Burst: 20},
	TierRelaxed: {Rate: 30, Burst: 50},
	TierUpload:  {Rate: 5, Burst: 10},
}

// tierLimiter je per-tier rate limiter s mapou IP → visitor.
type tierLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     float64
	burst    int
}

func newTierLimiter(cfg tierConfig) *tierLimiter {
	return &tierLimiter{
		visitors: make(map[string]*visitor),
		rate:     cfg.Rate,
		burst:    cfg.Burst,
	}
}

func (tl *tierLimiter) allow(ip string) bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	v, exists := tl.visitors[ip]
	now := time.Now()

	if !exists {
		tl.visitors[ip] = &visitor{tokens: float64(tl.burst) - 1, lastSeen: now}
		return true
	}

	elapsed := now.Sub(v.lastSeen).Seconds()
	v.tokens += elapsed * tl.rate
	if v.tokens > float64(tl.burst) {
		v.tokens = float64(tl.burst)
	}
	v.lastSeen = now

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

// cleanup smaže visitory neviděné déle než 5 minut.
func (tl *tierLimiter) cleanup() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	for ip, v := range tl.visitors {
		if time.Since(v.lastSeen) > 5*time.Minute {
			delete(tl.visitors, ip)
		}
	}
}

// pathTierRule definuje pravidlo pro přiřazení tieru cestě.
type pathTierRule struct {
	// Prefix cesty (matchuje strings.HasPrefix).
	Prefix string
	// Method — pokud prázdný, matchuje všechny metody.
	Method string
	// Tier pro toto pravidlo.
	Tier Tier
}

// EndpointRateLimiter rozřazuje requesty do tierů podle cesty a metody,
// každý tier má vlastní per-IP token bucket.
type EndpointRateLimiter struct {
	limiters map[Tier]*tierLimiter
	rules    []pathTierRule
}

// NewEndpointRateLimiter vytvoří nový per-endpoint rate limiter.
// normalRate a normalBurst přepíšou default pro TierNormal
// (zachovává hodnoty z nora.toml configu).
func NewEndpointRateLimiter(normalRate float64, normalBurst int) *EndpointRateLimiter {
	configs := make(map[Tier]tierConfig)
	for t, c := range defaultTierConfigs {
		configs[t] = c
	}
	// Přepsat normal tier hodnotami z configu
	configs[TierNormal] = tierConfig{Rate: normalRate, Burst: normalBurst}

	limiters := make(map[Tier]*tierLimiter)
	for t, c := range configs {
		limiters[t] = newTierLimiter(c)
	}

	erl := &EndpointRateLimiter{
		limiters: limiters,
		rules:    buildRules(),
	}

	// GC goroutine — každou minutu vyčistit staré visitory
	go erl.cleanup()

	return erl
}

// buildRules vrací pravidla pro přiřazení cest k tierům.
// Pravidla se prochází od prvního k poslednímu, první match vyhrává.
// Specifičtější pravidla musí být první.
func buildRules() []pathTierRule {
	return []pathTierRule{
		// Strict: auth endpointy
		{Prefix: "/api/auth/challenge", Tier: TierStrict},
		{Prefix: "/api/auth/verify", Tier: TierStrict},
		{Prefix: "/api/auth/refresh", Tier: TierStrict},

		// Relaxed: servování nahraných souborů (musí být před /api/upload)
		{Prefix: "/api/uploads/", Method: "GET", Tier: TierRelaxed},

		// Upload: nahrávání souborů
		{Prefix: "/api/upload", Tier: TierUpload},
		{Prefix: "/api/emojis", Method: "POST", Tier: TierUpload},
		{Prefix: "/api/users/me/avatar", Method: "POST", Tier: TierUpload},
		{Prefix: "/api/server/icon", Method: "POST", Tier: TierUpload},
		{Prefix: "/api/storage/files", Method: "POST", Tier: TierUpload},
		// Game server file upload (cesta obsahuje /files/upload)
		{Prefix: "/api/gameservers/", Tier: TierUpload}, // jen pro upload — viz resolveTier

		// Relaxed: čtení, listing, health, WS
		{Prefix: "/api/health", Tier: TierRelaxed},
		{Prefix: "/api/server", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/ws", Tier: TierRelaxed},
		{Prefix: "/api/channels", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/users", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/source", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/voice/state", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/gallery", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/emojis", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/roles", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/friends", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/dm", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/groups", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/categories", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/whiteboards", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/kanban", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/events", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/invites", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/bans", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/shares", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/devices", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/blocks", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/webhooks", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/scheduled-messages", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/invite-chain", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/quarantine", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/approvals", Method: "GET", Tier: TierRelaxed},
		{Prefix: "/api/messages/", Method: "GET", Tier: TierRelaxed},

		// Vše ostatní → TierNormal (nemusí být v rules, je default)
	}
}

// resolveTier určí tier pro daný request podle cesty a metody.
func (erl *EndpointRateLimiter) resolveTier(method, path string) Tier {
	for _, rule := range erl.rules {
		// Speciální pravidlo pro gameservers upload —
		// matchujeme jen pokud cesta obsahuje /files/upload
		if rule.Prefix == "/api/gameservers/" {
			if strings.Contains(path, "/files/upload") {
				return rule.Tier
			}
			continue
		}

		if !strings.HasPrefix(path, rule.Prefix) {
			continue
		}
		if rule.Method != "" && rule.Method != method {
			continue
		}
		return rule.Tier
	}
	return TierNormal
}

// Middleware vrací HTTP middleware s per-endpoint rate limitingem.
func (erl *EndpointRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := util.GetClientIP(r)
		tier := erl.resolveTier(r.Method, r.URL.Path)
		limiter := erl.limiters[tier]

		if !limiter.allow(ip) {
			util.Error(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// cleanup periodicky čistí staré visitory ze všech tierů.
func (erl *EndpointRateLimiter) cleanup() {
	for {
		time.Sleep(time.Minute)
		for _, tl := range erl.limiters {
			tl.cleanup()
		}
	}
}
