package hub

import (
	"encoding/json"
	"math/rand"
	"sync"
	"time"
)

type Sampler struct {
	intervalMs int
	filter     *fieldFilter

	mu       sync.Mutex
	sample   json.RawMessage
	count    int
	lastSent time.Time
}

func NewSampler(intervalMs int, filter *fieldFilter) *Sampler {
	return &Sampler{
		intervalMs: intervalMs,
		filter:     filter,
		lastSent:   time.Now(),
	}
}

func (s *Sampler) Add(msg json.RawMessage) (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.count++

	if rand.Intn(s.count) == 0 {
		s.sample = msg
	}

	if time.Since(s.lastSent).Milliseconds() >= int64(s.intervalMs) && s.count > 0 {
		return s.flush()
	}

	return nil, false
}

func (s *Sampler) Check() (json.RawMessage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.lastSent).Milliseconds() >= int64(s.intervalMs) && s.count > 0 {
		return s.flush()
	}

	return nil, false
}

func (s *Sampler) flush() (json.RawMessage, bool) {
	if s.count == 0 || s.sample == nil {
		return nil, false
	}

	var data map[string]interface{}
	if err := json.Unmarshal(s.sample, &data); err != nil {
		return nil, false
	}

	data["sample_count"] = s.count

	result, err := json.Marshal(data)
	if err != nil {
		return nil, false
	}

	s.count = 0
	s.sample = nil
	s.lastSent = time.Now()

	return result, true
}
