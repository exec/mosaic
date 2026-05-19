package api

import "sync"

// callerCache memoizes the Caller identity resolved for a user id. Every
// authenticated HTTP request and (on mosaicd) every per-user WebSocket tick
// would otherwise re-run a users-table SELECT to rebuild a Caller that almost
// never changes between requests.
//
// Correctness rests on aggressive invalidation: any mutation that could change
// a user's authority — role/permission/disable edits, password resets, API-key
// rotation, deletion — already revokes that user's live sessions, and the same
// call sites now also evict the cache entry (see Service.invalidateCaller).
// The cache therefore only ever holds Callers for enabled users, and a stale
// entry cannot outlive the change that invalidated it.
type callerCache struct {
	mu sync.RWMutex
	m  map[int]Caller
}

func newCallerCache() *callerCache {
	return &callerCache{m: make(map[int]Caller)}
}

// get returns the cached Caller for id, if present.
func (c *callerCache) get(id int) (Caller, bool) {
	c.mu.RLock()
	caller, ok := c.m[id]
	c.mu.RUnlock()
	return caller, ok
}

// put stores caller under id.
func (c *callerCache) put(id int, caller Caller) {
	c.mu.Lock()
	c.m[id] = caller
	c.mu.Unlock()
}

// evict drops the entry for id (a no-op if absent).
func (c *callerCache) evict(id int) {
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}
