package main

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	loginMaxFailuresPerWindow = 5
	loginFailureWindow        = 15 * time.Minute
)

type ipLoginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newIPLoginLimiter() *ipLoginLimiter {
	return &ipLoginLimiter{failures: make(map[string][]time.Time)}
}

// loginLimiter limits failed admin password attempts per client IP.
var loginLimiter = newIPLoginLimiter()

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (l *ipLoginLimiter) isBlocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-loginFailureWindow)
	var recent int
	for _, t := range l.failures[ip] {
		if t.After(cutoff) {
			recent++
		}
	}
	return recent >= loginMaxFailuresPerWindow
}

func (l *ipLoginLimiter) recordFailure(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-loginFailureWindow)
	recent := l.failures[ip][:0]
	for _, t := range l.failures[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	recent = append(recent, now)
	l.failures[ip] = recent
}

func (l *ipLoginLimiter) clear(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}
