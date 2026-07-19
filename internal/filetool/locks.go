package filetool

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

func newLockManager() *lockManager {
	return &lockManager{locks: make(map[string]*pathLock)}
}

func (m *lockManager) acquire(paths []string) func() {
	paths = uniqueSorted(paths)
	entries := make([]*pathLock, 0, len(paths))
	m.mu.Lock()
	for _, path := range paths {
		entry := m.locks[path]
		if entry == nil {
			entry = &pathLock{}
			m.locks[path] = entry
		}
		entry.refs++
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		entry.mu.Lock()
	}
	return func() {
		for index := len(entries) - 1; index >= 0; index-- {
			entries[index].mu.Unlock()
		}
		m.mu.Lock()
		for index, path := range paths {
			entries[index].refs--
			if entries[index].refs == 0 {
				delete(m.locks, path)
			}
		}
		m.mu.Unlock()
	}
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
