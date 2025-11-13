package engine

import (
	"log"
	"sync"
	"time"
)

// Runs a worker goroutine to process events.
func (s *SyncEngine) worker(id int) {
	defer s.workerWG.Done()
	for {
		select {
		case <-s.stopCh:
			return
		case qe, ok := <-s.jobs:
			if !ok {
				return
			}
			if qe.raw.Name == "" {
				continue
			}
			s.processEventWithLock(qe)
		}
	}
}

// Returns a lock for a specific relative path.
func (s *SyncEngine) lockFor(relPath string) *sync.Mutex {
	val, _ := s.perFileLocks.LoadOrStore(relPath, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// Processes an event with a lock.
func (s *SyncEngine) processEventWithLock(event queuedEvent) {
	if event.raw.Name == "" {
		return
	}

	lock := s.lockFor(event.relPath)
	lock.Lock()
	defer func() {
		lock.Unlock()
		s.markEventProcessed(event.isLocal, event.relPath)
	}()

	if err := s.handleQueuedEvent(event); err != nil {
		side := "remote"
		if event.isLocal {
			side = "local"
		}
		log.Printf("error handling %s event for %s (rel: %s): %v", side, event.raw.Name, event.relPath, err)
	}
}

// Handles a queued event.
func (s *SyncEngine) handleQueuedEvent(event queuedEvent) error {
	return s.handleEvent(event.raw)
}

// Checks if an event should be enqueued.
func (s *SyncEngine) shouldEnqueue(event queuedEvent) bool {
	if event.raw.Name == "" {
		return false
	}
	select {
	case <-s.stopCh:
		return false
	default:
	}
	key := s.eventKey(event.isLocal, event.relPath)
	now := time.Now()
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	last, exists := s.pendingEvents[key]
	if exists && now.Sub(last) < s.debounceInterval {
		return false
	}
	s.pendingEvents[key] = now
	return true
}

// Returns a key for an event.
func (s *SyncEngine) eventKey(isLocal bool, rel string) string {
	side := "remote"
	if isLocal {
		side = "local"
	}
	return side + ":" + rel
}

// Marks an event as processed.
func (s *SyncEngine) markEventProcessed(isLocal bool, rel string) {
	key := s.eventKey(isLocal, rel)
	s.debounceMu.Lock()
	s.pendingEvents[key] = time.Now()
	s.debounceMu.Unlock()
}
