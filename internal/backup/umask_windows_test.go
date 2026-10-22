package backup

import "testing"

// Windows uses ACLs rather than a Unix process umask.
func restrictUmask(t *testing.T) { t.Helper() }
