package relay

import "sync"

// hub wakes long-pollers. Each session holds at most one channel, which is
// closed and dropped on publish. Waiters subscribe before reading the log,
// so an append between the read and the select cannot be missed.
type hub struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

func (h *hub) sub(uid string) <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.m[uid]
	if !ok {
		ch = make(chan struct{})
		h.m[uid] = ch
	}
	return ch
}

func (h *hub) pub(uid string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.m[uid]; ok {
		close(ch)
		delete(h.m, uid)
	}
}
