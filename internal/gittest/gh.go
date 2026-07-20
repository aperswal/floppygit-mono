package gittest

import "fmt"

// GHCall is one recorded invocation against the fake gh runner. Tests assert on
// the ordered sequence of calls to prove push walks the stack bottom-up, that
// auto-merge is armed only on the bottom PR, and that each level's PR carries
// the right base, title, and body.
type GHCall struct {
	Op     string // "view", "create", "edit", "automerge", "watch", "merge"
	Branch string
	Base   string
	Title  string
	Body   string
}

// FakeGH implements floppygit's gh interface (gitexec.GH) with no network. A
// compile-time assertion in the package tests pins its method set to that
// interface, so the fake and the real seam cannot drift.
type FakeGH struct {
	// Existing lists head branches that already have an open PR, so ViewPR
	// reports true and the caller re-syncs via EditPR instead of CreatePR.
	Existing map[string]bool
	// AutoMergeErr, when set, makes AutoMerge fail so the checks-watch fallback
	// (WatchChecks then Merge) can be exercised.
	AutoMergeErr error
	// Calls is the ordered log of every call the code under test made.
	Calls []GHCall
}

// NewFakeGH returns a ready fake with an initialized Existing map.
func NewFakeGH() *FakeGH {
	return &FakeGH{Existing: map[string]bool{}}
}

func (f *FakeGH) record(c GHCall) { f.Calls = append(f.Calls, c) }

// ViewPR reports whether an open PR already exists for the head branch.
func (f *FakeGH) ViewPR(branch string) (bool, error) {
	f.record(GHCall{Op: "view", Branch: branch})
	return f.Existing[branch], nil
}

// CreatePR opens a PR for branch against base with the given title and body.
func (f *FakeGH) CreatePR(branch, base, title, body string) error {
	f.record(GHCall{Op: "create", Branch: branch, Base: base, Title: title, Body: body})
	f.Existing[branch] = true
	return nil
}

// EditPR re-syncs an existing PR's base, title, and body after an amend.
func (f *FakeGH) EditPR(branch, base, title, body string) error {
	f.record(GHCall{Op: "edit", Branch: branch, Base: base, Title: title, Body: body})
	return nil
}

// AutoMerge arms auto-merge (squash, delete branch) for branch.
func (f *FakeGH) AutoMerge(branch string) error {
	f.record(GHCall{Op: "automerge", Branch: branch})
	return f.AutoMergeErr
}

// WatchChecks blocks until branch's checks finish; the auto-merge fallback.
func (f *FakeGH) WatchChecks(branch string) error {
	f.record(GHCall{Op: "watch", Branch: branch})
	return nil
}

// Merge merges branch now (squash, delete branch); used after checks pass.
func (f *FakeGH) Merge(branch string) error {
	f.record(GHCall{Op: "merge", Branch: branch})
	return nil
}

// Ops returns just the ordered operation names, for concise sequence asserts.
func (f *FakeGH) Ops() []string {
	ops := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		ops[i] = c.Op
	}
	return ops
}

// Find returns the first recorded call matching op and branch.
func (f *FakeGH) Find(op, branch string) (GHCall, bool) {
	for _, c := range f.Calls {
		if c.Op == op && c.Branch == branch {
			return c, true
		}
	}
	return GHCall{}, false
}

// CountOp returns how many recorded calls used the given op.
func (f *FakeGH) CountOp(op string) int {
	n := 0
	for _, c := range f.Calls {
		if c.Op == op {
			n++
		}
	}
	return n
}

// Writes returns the ordered create/edit calls, the ones that mutate a PR's
// base/title/body, so a test can assert per-level payloads and walk order in one
// place.
func (f *FakeGH) Writes() []GHCall {
	var w []GHCall
	for _, c := range f.Calls {
		if c.Op == "create" || c.Op == "edit" {
			w = append(w, c)
		}
	}
	return w
}

// String renders the call log for failure messages.
func (f *FakeGH) String() string {
	s := ""
	for _, c := range f.Calls {
		s += fmt.Sprintf("%s(branch=%s base=%s title=%q)\n", c.Op, c.Branch, c.Base, c.Title)
	}
	return s
}
