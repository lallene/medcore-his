package access

import "sync"

var (
	runtimeMu sync.RWMutex
	runtime   *Service
)

// SetRuntime wires the access service for auth middleware permission recompute.
func SetRuntime(s *Service) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtime = s
}

// ComputeEffectiveForUser is used by auth middleware when the access module is loaded.
func ComputeEffectiveForUser(userID uint) ([]string, bool) {
	runtimeMu.RLock()
	s := runtime
	runtimeMu.RUnlock()
	if s == nil {
		return nil, false
	}
	perms, err := s.ComputeEffectivePermissions(userID)
	if err != nil {
		return nil, false
	}
	return perms, true
}
