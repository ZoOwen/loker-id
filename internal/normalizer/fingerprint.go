package normalizer

import (
	"crypto/sha256"
	"encoding/hex"
)

// Fingerprint is the dedup key for a job posting: sha256 hex of the
// normalized company name concatenated with the normalized title.
func Fingerprint(companyNormalized, titleNormalized string) string {
	sum := sha256.Sum256([]byte(companyNormalized + titleNormalized))
	return hex.EncodeToString(sum[:])
}
