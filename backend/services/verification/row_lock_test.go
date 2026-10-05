package verification

import "testing"

func TestVerificationLockQuery(t *testing.T) {
	const query = "SELECT * FROM verification_rate_limit WHERE dimension_key = ?"
	for _, driver := range []string{"postgres", "mysql", "sqlite", "sqlite3"} {
		t.Run(driver, func(t *testing.T) {
			got, err := verificationLockQuery(driver, query)
			if err != nil {
				t.Fatal(err)
			}
			want := query
			if driver == "postgres" || driver == "mysql" {
				want += " FOR UPDATE"
			}
			if got != want {
				t.Fatalf("query = %q, want %q", got, want)
			}
		})
	}
	if got, err := verificationLockQuery("unknown", query); err == nil || got != "" {
		t.Fatalf("unsupported driver did not fail closed: %q, %v", got, err)
	}
}
