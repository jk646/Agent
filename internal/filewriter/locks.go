package filewriter

import (
	"sort"
	"sync"
)

type pathLock struct {
	mu   sync.Mutex
	refs int
}
type lockManager struct {
	mu    sync.Mutex
	locks map[string]*pathLock
}

func newLockManager() *lockManager { return &lockManager{locks: make(map[string]*pathLock)} }

func (m *lockManager) lock(paths []string) func() {
	paths = uniqueSorted(paths)
	acquired := make([]*pathLock, 0, len(paths))
	for _, path := range paths {
		m.mu.Lock()
		item := m.locks[path]
		if item == nil {
			item = &pathLock{}
			m.locks[path] = item
		}
		item.refs++
		m.mu.Unlock()
		item.mu.Lock()
		acquired = append(acquired, item)
	}
	return func() {
		for index := len(acquired) - 1; index >= 0; index-- {
			acquired[index].mu.Unlock()
		}
		m.mu.Lock()
		for index, path := range paths {
			item := acquired[index]
			item.refs--
			if item.refs == 0 {
				delete(m.locks, path)
			}
		}
		m.mu.Unlock()
	}
}
func uniqueSorted(values []string) []string {
	set := make(map[string]struct{})
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
