// Copyright Antimetal, Inc. All rights reserved.
//
// Use of this source code is governed by a source available license that can be found in the
// LICENSE file or at:
// https://polyformproject.org/wp-content/uploads/2020/06/PolyForm-Shield-1.0.0.txt

package config

import "sync"

func Matches(instance Instance, filters Filters) bool {
	if filters.Status != 0 && (instance.Status&filters.Status) == 0 {
		return false
	}

	if len(filters.Types) == 0 {
		return true
	}

	for _, t := range filters.Types {
		if t == instance.TypeUrl {
			return true
		}
	}
	return false
}

type subscription struct {
	ch      chan Instance
	filters Filters
}

type subscriptions struct {
	mu     sync.RWMutex
	subs   []subscription
	closed bool
}

func (s *subscriptions) add(filters Filters) chan Instance {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	if filters.Status == 0 {
		filters.Status = StatusOK
	}

	ch := make(chan Instance, 10)
	s.subs = append(s.subs, subscription{
		ch:      ch,
		filters: filters,
	})
	return ch
}

func (s *subscriptions) send(instances ...Instance) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	for _, sub := range s.subs {
		for _, instance := range instances {
			if Matches(instance, sub.filters) {
				sub.ch <- instance.Copy()
			}
		}
	}
}

func (s *subscriptions) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	for _, sub := range s.subs {
		close(sub.ch)
	}
	s.closed = true
}
