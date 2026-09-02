package gitx

import (
	"context"
	"strings"
	"sync"
)

// Fake is an in-memory Runner for unit tests. Responses are keyed by a
// space-joined prefix of the args (longest prefix wins); unmatched
// commands succeed with empty output.
type Fake struct {
	mu        sync.Mutex
	Responses map[string]FakeResponse
	Calls     []Call
}

type FakeResponse struct {
	Out string
	Err error
}

func NewFake() *Fake {
	return &Fake{Responses: map[string]FakeResponse{}}
}

func (f *Fake) Run(ctx context.Context, dir string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Dir: dir, Args: args})
	joined := strings.Join(args, " ")
	best := ""
	for k := range f.Responses {
		if strings.HasPrefix(joined, k) && len(k) > len(best) {
			best = k
		}
	}
	if best != "" {
		r := f.Responses[best]
		return r.Out, r.Err
	}
	return "", nil
}

// CommandLines renders calls as "dir: git args..." for assertions.
func (f *Fake) CommandLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Calls {
		out = append(out, c.Dir+": git "+strings.Join(c.Args, " "))
	}
	return out
}
