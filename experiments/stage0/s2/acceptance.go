package s2

// Acceptance returns the held-out workload in a stable order. Its tasks are written without
// sight of the harness or of any result, and nothing was tuned on them. Tasks and Hash do not
// cover it, so adding to it leaves the tuning set's frozen hash alone.
func Acceptance() []Task {
	var out []Task
	for _, group := range [][]func() Task{acceptanceA, acceptanceB, acceptanceC} {
		for _, task := range group {
			out = append(out, task())
		}
	}
	return out
}

// AcceptanceHash returns the SHA-256 of the canonical JSON of Acceptance.
func AcceptanceHash() string { return hashTasks(Acceptance()) }
