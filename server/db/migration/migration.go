package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

type Migration struct {
	Version           int64
	Name              string
	Statements        []string
	DialectStatements map[Dialect][]string
	ChecksumSeed      string
	Apply             func(context.Context, executor, Dialect) error
}

func Define(version int64, name string, statements ...string) Migration {
	copied := append([]string(nil), statements...)
	return Migration{Version: version, Name: strings.TrimSpace(name), Statements: copied}
}

func DefineForDialects(version int64, name string, statements map[Dialect][]string) Migration {
	copied := make(map[Dialect][]string, len(statements))
	for dialect, items := range statements {
		copied[dialect] = append([]string(nil), items...)
	}
	return Migration{Version: version, Name: strings.TrimSpace(name), DialectStatements: copied}
}

func DefineCustom(version int64, name string, checksumSeed string, apply func(context.Context, executor, Dialect) error) Migration {
	return Migration{
		Version:      version,
		Name:         strings.TrimSpace(name),
		ChecksumSeed: strings.TrimSpace(checksumSeed),
		Apply:        apply,
	}
}

func (m Migration) StatementsFor(dialect Dialect) []string {
	if len(m.DialectStatements) > 0 {
		return append([]string(nil), m.DialectStatements[dialect]...)
	}
	return append([]string(nil), m.Statements...)
}

func (m Migration) Checksum() string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%d\x00%s\x00", m.Version, m.Name)
	for _, statement := range m.Statements {
		_, _ = hash.Write([]byte(strings.TrimSpace(statement)))
		_, _ = hash.Write([]byte{0})
	}
	if m.ChecksumSeed != "" {
		_, _ = hash.Write([]byte(m.ChecksumSeed))
		_, _ = hash.Write([]byte{0})
	}
	keys := make([]string, 0, len(m.DialectStatements))
	for dialect := range m.DialectStatements {
		keys = append(keys, string(dialect))
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = hash.Write([]byte(key))
		_, _ = hash.Write([]byte{0})
		for _, statement := range m.DialectStatements[Dialect(key)] {
			_, _ = hash.Write([]byte(strings.TrimSpace(statement)))
			_, _ = hash.Write([]byte{0})
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func normalize(migrations []Migration) ([]Migration, error) {
	result := append([]Migration(nil), migrations...)
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for i, item := range result {
		if i == 0 && item.Version != 1 {
			return nil, fmt.Errorf("migration sequence must start at version 1")
		}
		if i > 0 && item.Version != result[i-1].Version+1 {
			return nil, fmt.Errorf("migration version %d is not contiguous", item.Version)
		}
		if item.Version <= 0 || item.Name == "" || !hasStatements(item) {
			return nil, fmt.Errorf("invalid migration definition at index %d", i)
		}
		for _, statement := range item.Statements {
			if strings.TrimSpace(statement) == "" {
				return nil, fmt.Errorf("migration %d contains an empty statement", item.Version)
			}
		}
		for dialect, statements := range item.DialectStatements {
			if _, err := ParseDialect(string(dialect)); err != nil {
				return nil, err
			}
			if len(statements) == 0 {
				return nil, fmt.Errorf("migration %d contains no statements for %s", item.Version, dialect)
			}
			for _, statement := range statements {
				if strings.TrimSpace(statement) == "" {
					return nil, fmt.Errorf("migration %d contains an empty statement", item.Version)
				}
			}
		}
		if i > 0 && result[i-1].Version == item.Version {
			return nil, fmt.Errorf("duplicate migration version %d", item.Version)
		}
	}
	return result, nil
}

func hasStatements(m Migration) bool {
	if m.Apply != nil && m.ChecksumSeed != "" {
		return true
	}
	if len(m.Statements) > 0 {
		return true
	}
	for _, statements := range m.DialectStatements {
		if len(statements) > 0 {
			return true
		}
	}
	return false
}
