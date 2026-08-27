package config

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestFixedSectionFiles(t *testing.T) {
	expected := map[Section]string{
		SectionRoot:    "config.json",
		SectionSMTP:    "smtp.json",
		SectionEmail:   "email.json",
		SectionSSL:     "ssl.json",
		SectionPlugins: "plugins.json",
	}
	for section, want := range expected {
		got, ok := FixedSectionFile(section)
		if !ok || got != want {
			t.Fatalf("FixedSectionFile(%q) = %q, %t", section, got, ok)
		}
	}
	if _, ok := FixedSectionFile(Section("../config")); ok {
		t.Fatal("unknown section received a file mapping")
	}
}

func TestValidateSectionReferences(t *testing.T) {
	generation := uint64(12)
	references := validSectionReferences(generation)
	if err := ValidateSectionReferences(references, generation); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(map[Section]SectionRef)
		want   error
	}{
		{name: "missing", mutate: func(items map[Section]SectionRef) { delete(items, SectionSMTP) }, want: ErrInvalidSectionReference},
		{name: "unknown", mutate: func(items map[Section]SectionRef) { items[Section("unknown")] = items[SectionSMTP] }, want: ErrInvalidSectionReference},
		{name: "path", mutate: func(items map[Section]SectionRef) {
			item := items[SectionSMTP]
			item.File = "../smtp.json"
			items[SectionSMTP] = item
		}, want: ErrInvalidSectionReference},
		{name: "schema", mutate: func(items map[Section]SectionRef) {
			item := items[SectionSMTP]
			item.Schema++
			items[SectionSMTP] = item
		}, want: ErrInvalidSectionReference},
		{name: "generation", mutate: func(items map[Section]SectionRef) {
			item := items[SectionSMTP]
			item.Generation++
			items[SectionSMTP] = item
		}, want: ErrSectionGenerationMismatch},
		{name: "digest", mutate: func(items map[Section]SectionRef) {
			item := items[SectionSMTP]
			item.SHA256 = "ABC"
			items[SectionSMTP] = item
		}, want: ErrInvalidSectionReference},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneSectionReferences(references)
			test.mutate(candidate)
			if err := ValidateSectionReferences(candidate, generation); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSectionMetaAndDigest(t *testing.T) {
	digest := SectionDigest([]byte(`{"meta":{"schema":1,"generation":3}}`))
	if !validSHA256(digest) {
		t.Fatalf("invalid digest %q", digest)
	}
	if err := ValidateSectionMeta(SectionSMTP, SectionMeta{Schema: CurrentSectionSchema, Generation: 3}, 3); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSectionMeta(SectionRoot, SectionMeta{Schema: CurrentSectionSchema, Generation: 3}, 3); !errors.Is(err, ErrInvalidSection) {
		t.Fatalf("error = %v", err)
	}
}

func TestRootConfigSectionReferencesRoundTrip(t *testing.T) {
	root := testRootConfig()
	root.Domains = DomainConfig{Primary: "mail.example.com", Accepted: []string{"example.com"}, Web: "mail.example.com"}
	root.HTTP = HTTPConfig{Port: 80, HTTPSPort: 443, HTTPSEnabled: true}
	root.Sections = validSectionReferences(root.Generation)
	data, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RootConfig
	if err := ParseStrict(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRootConfig(decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Sections[SectionPlugins].File != "plugins.json" {
		t.Fatalf("decoded references = %+v", decoded.Sections)
	}
}

func TestRootConfigCloneDoesNotShareSectionData(t *testing.T) {
	root := testRootConfig()
	root.Domains.Accepted = []string{"example.com"}
	root.Sections = validSectionReferences(root.Generation)
	clone := cloneRootConfig(root)
	clone.Domains.Accepted[0] = "attacker.example"
	item := clone.Sections[SectionSMTP]
	item.File = "changed.json"
	clone.Sections[SectionSMTP] = item
	if root.Domains.Accepted[0] != "example.com" || root.Sections[SectionSMTP].File != "smtp.json" {
		t.Fatal("root clone shares mutable section data")
	}
}

func validSectionReferences(generation uint64) map[Section]SectionRef {
	result := make(map[Section]SectionRef, len(requiredPartitionSections))
	for _, section := range requiredPartitionSections {
		file, _ := FixedSectionFile(section)
		schema, _ := FixedSectionSchema(section)
		result[section] = SectionRef{File: file, Schema: schema, Generation: generation, SHA256: SectionDigest([]byte(section))}
	}
	return result
}

func cloneSectionReferences(source map[Section]SectionRef) map[Section]SectionRef {
	result := make(map[Section]SectionRef, len(source))
	for section, reference := range source {
		result[section] = reference
	}
	return result
}
