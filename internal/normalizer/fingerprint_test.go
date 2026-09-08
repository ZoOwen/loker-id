package normalizer

import "testing"

func TestFingerprint(t *testing.T) {
	// Golden value: sha256("gojek indonesia" + "backenddeveloper") computed
	// independently via `sha256sum`.
	const want = "01066f2ff0a0415e695bdef3404125a15643f7cf1fc613b7f5a389c63bb20266"

	got := Fingerprint("gojek indonesia", "backenddeveloper")
	if got != want {
		t.Errorf("Fingerprint(%q, %q) = %q, want %q", "gojek indonesia", "backenddeveloper", got, want)
	}
}

func TestFingerprint_Deterministic(t *testing.T) {
	a := Fingerprint("gojek indonesia", "backenddeveloper")
	b := Fingerprint("gojek indonesia", "backenddeveloper")
	if a != b {
		t.Errorf("Fingerprint is not deterministic: %q != %q", a, b)
	}
}

func TestFingerprint_DifferentInputsDifferentHash(t *testing.T) {
	a := Fingerprint("gojek indonesia", "backenddeveloper")
	b := Fingerprint("tokopedia", "backenddeveloper")
	if a == b {
		t.Errorf("Fingerprint(%q, ...) and Fingerprint(%q, ...) collided: %q", "gojek indonesia", "tokopedia", a)
	}
}

func TestFingerprint_Format(t *testing.T) {
	got := Fingerprint("acme", "backenddeveloper")
	if len(got) != 64 {
		t.Errorf("len(Fingerprint(...)) = %d, want 64 (sha256 hex)", len(got))
	}
	for _, r := range got {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Errorf("Fingerprint(...) = %q, contains non-lowercase-hex character %q", got, r)
			break
		}
	}
}
