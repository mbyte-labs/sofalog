package common

// KickTMDBSync wakes the worker without blocking: the channel holds one
// pending kick, and a second while one waits is redundant. Nil-safe, because
// a Core built by hand in a test has no channel. Called AFTER a transaction
// commits, never inside one — the worker must not race the row's own commit.
func (s *Core) KickTMDBSync() {
	if s.TMDBSyncKick == nil {
		return
	}
	select {
	case s.TMDBSyncKick <- struct{}{}:
	default:
	}
}
