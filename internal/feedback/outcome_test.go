package feedback

import "testing"

func TestFingerprintNormalises(t *testing.T) {
	a := Fingerprint("Add error handling to main.go")
	b := Fingerprint("add error handling  to  Main.go")
	if a != b {
		t.Fatalf("fingerprints differ for equivalent input: %q vs %q", a, b)
	}
}

func TestFingerprintDiffersForDifferentInput(t *testing.T) {
	a := Fingerprint("Add error handling to main.go")
	b := Fingerprint("Remove error handling from main.go")
	if a == b {
		t.Fatalf("fingerprints unexpectedly equal for different input")
	}
}
