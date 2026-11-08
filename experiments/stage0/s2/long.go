package s2

// Long returns the long-task workload in a stable order: sessions that run several times past
// a small window and depend, late on, on what was settled early. Its tasks are written without
// sight of the harness or of any result. Tasks, Acceptance and their hashes do not cover it.
func Long() []Task {
	var out []Task
	for _, group := range [][]func() Task{longA, longB, longC} {
		for _, task := range group {
			out = append(out, task())
		}
	}
	return out
}

// LongHash returns the SHA-256 of the canonical JSON of Long.
func LongHash() string { return hashTasks(Long()) }
