package main

import (
	"fmt"
	"strings"
	"time"
)

// EnvNotificationWorkerPoll is the worker-only poll interval (Go duration).
const EnvNotificationWorkerPoll = "NOTIFICATION_WORKER_POLL"

// ParseNotificationWorkerPoll interprets NOTIFICATION_WORKER_POLL.
// Unset/empty/whitespace → fallback (typically 2s). Explicit malformed or
// non-positive values return an error (fail closed; never silent fallback).
func ParseNotificationWorkerPoll(raw string, fallback time.Duration) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: valeur invalide (durée Go positive attendue, ex. 2s)", EnvNotificationWorkerPoll)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s: durée non positive (durée Go positive attendue, ex. 2s)", EnvNotificationWorkerPoll)
	}
	return d, nil
}
