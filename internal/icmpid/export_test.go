package icmpid

// Held returns the number of IDs currently held.
func (a *Allocator) Held() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.held)
}
