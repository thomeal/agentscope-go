package s2

// Long2 returns the second long-task workload: new tasks under the long set's rules, written by
// new authors with the same blindness after the long set had run, so a result on the long set
// can be checked on tasks no model has seen. Its tasks have Set "long" and keep the long set's
// limits. Tasks, Acceptance, Long and their hashes do not cover it.
func Long2() []Task {
	var out []Task
	for _, group := range [][]func() Task{long2A, long2B, long2C} {
		for _, task := range group {
			out = append(out, task())
		}
	}
	return out
}

// Long2Hash returns the SHA-256 of the canonical JSON of Long2.
func Long2Hash() string { return hashTasks(Long2()) }
