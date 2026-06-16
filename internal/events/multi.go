package events

// multiEmitter fans out Emit calls to all provided emitters.
type multiEmitter struct{ emitters []Emitter }

// Multi returns an Emitter that forwards every event to all provided
// emitters in order. Useful for combining a human-readable renderer
// with a debug JSON emitter in headless mode.
func Multi(emitters ...Emitter) Emitter {
	return &multiEmitter{emitters: emitters}
}

func (m *multiEmitter) Emit(e Event) {
	for _, em := range m.emitters {
		em.Emit(e)
	}
}
