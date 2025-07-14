package instrumentor

import "encoding/json"

type Static struct{}

func (s *Static) GetEffectiveConfig() []byte {
	// Return a static configuration for testing purposes
	return json.RawMessage([]byte(`{"collectors": ["load"]}`))
}

func (s *Static) Status() Status {
	return StatusApplied
}
