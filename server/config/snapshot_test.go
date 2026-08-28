package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestSnapshotStoreRejectsInvalidCandidate(t *testing.T) {
	initial := validRuntimeConfig()
	store, err := NewSnapshotStore(initial, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}
	invalid := validRuntimeConfig()
	invalid.Email.Policy.IMAPPort = invalid.Root.HTTP.HTTPSPort
	if err := store.Publish(invalid); err == nil {
		t.Fatal("invalid runtime configuration was published")
	}
	if store.Generation() != initial.Generation {
		t.Fatal("rejected publication changed the active generation")
	}
}

func TestSnapshotStoreReturnsDeepCopies(t *testing.T) {
	initial := validRuntimeConfig()
	store, err := NewSnapshotStore(initial, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}

	initial.Root.Domains.Accepted[0] = "mutated.example.com"
	initial.Root.Sections[SectionSMTP] = SectionRef{}
	plugin := initial.Plugins.Plugins["telegram_push"]
	plugin.Permissions.Capabilities[0] = "filesystem"
	plugin.Permissions.Network[0].Host = "mutated.example.com"
	plugin.Settings[0] = '['
	initial.Plugins.Plugins["telegram_push"] = plugin

	first, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.Root.Domains.Accepted[0] != "mail.example.com" || first.Root.Sections[SectionSMTP].File != "smtp.json" {
		t.Fatal("published root snapshot shares mutable input data")
	}
	firstPlugin := first.Plugins.Plugins["telegram_push"]
	if firstPlugin.Permissions.Capabilities[0] != "network" || firstPlugin.Permissions.Network[0].Host != "api.telegram.org" || !json.Valid(firstPlugin.Settings) {
		t.Fatal("published plugin snapshot shares mutable input data")
	}

	first.Root.Domains.Accepted[0] = "reader.example.com"
	firstPlugin.Settings[0] = '['
	first.Plugins.Plugins["telegram_push"] = firstPlugin
	second, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if second.Root.Domains.Accepted[0] != "mail.example.com" || !json.Valid(second.Plugins.Plugins["telegram_push"].Settings) {
		t.Fatal("caller modified the active snapshot")
	}
}

func TestSnapshotStoreConcurrentReadersAndUpdates(t *testing.T) {
	initial := validRuntimeConfig()
	store, err := NewSnapshotStore(initial, RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}

	const updates = 64
	const readers = 24
	var group sync.WaitGroup
	errors := make(chan error, updates+readers)

	for reader := 0; reader < readers; reader++ {
		group.Add(1)
		go func() {
			defer group.Done()
			var previous uint64
			for iteration := 0; iteration < updates*4; iteration++ {
				snapshot, err := store.Snapshot()
				if err != nil {
					errors <- err
					return
				}
				if snapshot.Generation < previous || snapshot.Root.Generation != snapshot.Generation || snapshot.SMTP.Meta.Generation != snapshot.Generation {
					errors <- fmt.Errorf("inconsistent runtime generation")
					return
				}
				previous = snapshot.Generation
			}
		}()
	}

	group.Add(1)
	go func() {
		defer group.Done()
		for generation := uint64(2); generation <= updates+1; generation++ {
			candidate := validRuntimeConfig()
			candidate.Generation = generation
			candidate.Root.Generation = generation
			candidate.Root.Sections = validSectionReferences(generation)
			candidate.SMTP.Meta.Generation = generation
			candidate.Email.Meta.Generation = generation
			candidate.SSL.Meta.Generation = generation
			candidate.Plugins.Meta.Generation = generation
			if err := store.Publish(candidate); err != nil {
				errors <- err
				return
			}
		}
	}()

	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	if store.Generation() != updates+1 {
		t.Fatalf("unexpected active generation: %d", store.Generation())
	}
}

func TestSnapshotStorePreservesOldSnapshotLifecycle(t *testing.T) {
	store, err := NewSnapshotStore(validRuntimeConfig(), RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	candidate := validRuntimeConfig()
	candidate.Generation = 2
	candidate.Root.Generation = 2
	candidate.Root.Sections = validSectionReferences(2)
	candidate.SMTP.Meta.Generation = 2
	candidate.Email.Meta.Generation = 2
	candidate.SSL.Meta.Generation = 2
	candidate.Plugins.Meta.Generation = 2
	if err := store.Publish(candidate); err != nil {
		t.Fatal(err)
	}
	if old.Generation != 1 || old.Root.Generation != 1 {
		t.Fatal("publishing a new snapshot modified an old reader copy")
	}
}
func TestSnapshotStoreRejectsOlderGeneration(t *testing.T) {
	store, err := NewSnapshotStore(validRuntimeConfig(), RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}

	newer := validRuntimeConfig()
	newer.Generation = 3
	newer.Root.Generation = 3
	newer.Root.Sections = validSectionReferences(3)
	newer.SMTP.Meta.Generation = 3
	newer.Email.Meta.Generation = 3
	newer.SSL.Meta.Generation = 3
	newer.Plugins.Meta.Generation = 3
	if err := store.Publish(newer); err != nil {
		t.Fatal(err)
	}

	older := validRuntimeConfig()
	older.Generation = 2
	older.Root.Generation = 2
	older.Root.Sections = validSectionReferences(2)
	older.SMTP.Meta.Generation = 2
	older.Email.Meta.Generation = 2
	older.SSL.Meta.Generation = 2
	older.Plugins.Meta.Generation = 2
	if err := store.Publish(older); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("older publication error = %v, want generation conflict", err)
	}
	if got := store.Generation(); got != 3 {
		t.Fatalf("active generation = %d, want 3", got)
	}
}

func TestSnapshotStoreConcurrentOlderPublishesKeepNewestGeneration(t *testing.T) {
	store, err := NewSnapshotStore(validRuntimeConfig(), RuntimeValidationOptions{HasSuperAdmin: true, Production: true})
	if err != nil {
		t.Fatal(err)
	}

	newer := validRuntimeConfig()
	newer.Generation = 3
	newer.Root.Generation = 3
	newer.Root.Sections = validSectionReferences(3)
	newer.SMTP.Meta.Generation = 3
	newer.Email.Meta.Generation = 3
	newer.SSL.Meta.Generation = 3
	newer.Plugins.Meta.Generation = 3
	if err := store.Publish(newer); err != nil {
		t.Fatal(err)
	}

	const publishers = 32
	var group sync.WaitGroup
	errorsFound := make(chan error, publishers)
	for index := 0; index < publishers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			older := validRuntimeConfig()
			older.Generation = 2
			older.Root.Generation = 2
			older.Root.Sections = validSectionReferences(2)
			older.SMTP.Meta.Generation = 2
			older.Email.Meta.Generation = 2
			older.SSL.Meta.Generation = 2
			older.Plugins.Meta.Generation = 2
			if err := store.Publish(older); !errors.Is(err, ErrGenerationConflict) {
				errorsFound <- err
			}
		}()
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("older concurrent publication error = %v", err)
	}
	if got := store.Generation(); got != 3 {
		t.Fatalf("active generation = %d, want 3", got)
	}
}
