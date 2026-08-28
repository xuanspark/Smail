package buildinfo

import "testing"

func TestCurrentUsesSafeDefaults(t *testing.T) {
	oldVersion, oldHash := Version, GitHash
	t.Cleanup(func() {
		Version, GitHash = oldVersion, oldHash
	})
	Version = ""
	GitHash = ""

	info := Current()
	if info.Version != "dev" || info.GitHash != "unknown" {
		t.Fatalf("unexpected defaults: %+v", info)
	}
}

func TestValidatePairRequiresMatchingBuildMetadata(t *testing.T) {
	base := Info{Version: "v1.2.3", GitHash: "abc", ProtocolVersion: ProtocolVersion}
	cases := []struct {
		name  string
		main  Info
		admin Info
		want  error
	}{
		{name: "matching", main: base, admin: base},
		{name: "version mismatch", main: base, admin: Info{Version: "v1.2.4", GitHash: "abc", ProtocolVersion: ProtocolVersion}, want: ErrVersionMismatch},
		{name: "protocol mismatch", main: base, admin: Info{Version: "v1.2.3", GitHash: "abc", ProtocolVersion: ProtocolVersion + 1}, want: ErrProtocolMismatch},
		{name: "build mismatch", main: base, admin: Info{Version: "v1.2.3", GitHash: "def", ProtocolVersion: ProtocolVersion}, want: ErrBuildMismatch},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePair(test.main, test.admin)
			if test.want == nil && err != nil {
				t.Fatalf("ValidatePair() error = %v", err)
			}
			if test.want != nil && err != test.want {
				t.Fatalf("ValidatePair() error = %v, want %v", err, test.want)
			}
		})
	}
}
