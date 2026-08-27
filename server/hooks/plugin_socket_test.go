package hooks

import (
	"strings"
	"testing"
)

func TestNewPluginSocketNameIsUnpredictable(t *testing.T) {
	first, err := newPluginSocketName(1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPluginSocketName(1)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "smail-1-") || len(first) < len("smail-1-")+32 {
		t.Fatalf("plugin socket names are not sufficiently unique: %q, %q", first, second)
	}
}

func TestAbstractPluginSocketDetection(t *testing.T) {
	if !isAbstractPluginSocket("@smail-test") || isAbstractPluginSocket("smail-test") {
		t.Fatal("abstract plugin socket detection is incorrect")
	}
}
