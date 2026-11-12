package semcp

import "testing"

// Tools that take a path on the server's filesystem need the admin scope; tools that change stored
// data need write; everything else reads.
func TestToolScopes(t *testing.T) {
	want := map[string]string{
		"audit_import":          ScopeAdmin,
		"audit_export":          ScopeAdmin,
		"finding_add":           ScopeWrite,
		"finding_update_status": ScopeWrite,
	}
	for _, name := range toolNames {
		scope := toolScope(name)
		if w, ok := want[name]; ok && scope != w {
			t.Errorf("%s: scope %s, want %s", name, scope, w)
		}
		if _, ok := want[name]; !ok && scope != ScopeRead {
			t.Errorf("%s: scope %s, want %s", name, scope, ScopeRead)
		}
	}
}
