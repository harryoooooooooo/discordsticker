package utils

import (
	"sync"
	"time"
)

type CoolDownCounter struct {
	mu    sync.Mutex
	items map[interface{}]bool
}

func NewCoolDownCounter() *CoolDownCounter {
	return &CoolDownCounter{
		items: make(map[interface{}]bool),
	}
}

// CoolDown marks item as cooling down.
// If item is already cooling down, the function is a no-op and returns false;
// Otherwise the function returns true.
func (c *CoolDownCounter) CoolDown(d time.Duration, item interface{}) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.items[item] {
		return false
	}

	c.items[item] = true
	go func() {
		time.Sleep(d)
		c.RemoveCoolDown(item)
	}()

	return true
}

// RemoveCoolDown removes item from the cooling down list.
// If item is already cooling down, the function is a no-op.
func (c *CoolDownCounter) RemoveCoolDown(item interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, item)
}
