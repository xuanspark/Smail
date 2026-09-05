package config

import (
	"errors"
	"sync/atomic"
)

var ErrRuntimeSnapshotUnavailable = errors.New("runtime configuration snapshot unavailable")

type SnapshotStore struct {
	current atomic.Pointer[RuntimeConfig]
	options RuntimeValidationOptions
}

func NewSnapshotStore(initial RuntimeConfig, options RuntimeValidationOptions) (*SnapshotStore, error) {
	store := &SnapshotStore{options: options}
	if err := store.Publish(initial); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SnapshotStore) Validate(candidate RuntimeConfig) error {
	if s == nil {
		return ErrRuntimeSnapshotUnavailable
	}
	return ValidateRuntimeConfig(candidate, s.options)
}

func (s *SnapshotStore) Publish(candidate RuntimeConfig) error {
	if s == nil {
		return ErrRuntimeSnapshotUnavailable
	}
	candidate = cloneRuntimeConfig(candidate)
	if err := s.Validate(candidate); err != nil {
		return err
	}
	for {
		current := s.current.Load()
		if current != nil && candidate.Generation < current.Generation {
			return ErrGenerationConflict
		}
		if s.current.CompareAndSwap(current, &candidate) {
			return nil
		}
	}
}

func (s *SnapshotStore) Snapshot() (RuntimeConfig, error) {
	if s == nil {
		return RuntimeConfig{}, ErrRuntimeSnapshotUnavailable
	}
	current := s.current.Load()
	if current == nil {
		return RuntimeConfig{}, ErrRuntimeSnapshotUnavailable
	}
	return cloneRuntimeConfig(*current), nil
}

func (s *SnapshotStore) Generation() uint64 {
	if s == nil {
		return 0
	}
	current := s.current.Load()
	if current == nil {
		return 0
	}
	return current.Generation
}

func cloneRuntimeConfig(source RuntimeConfig) RuntimeConfig {
	clone := source
	clone.Root = cloneRootConfig(source.Root)
	clone.SMTP = source.SMTP
	clone.Email = source.Email
	clone.SSL = source.SSL
	clone.Plugins = clonePluginsSection(source.Plugins)
	return clone
}

func clonePluginsSection(source PluginsSectionConfig) PluginsSectionConfig {
	clone := source
	if source.Plugins == nil {
		return clone
	}
	clone.Plugins = make(map[string]PluginInstance, len(source.Plugins))
	for logicalID, plugin := range source.Plugins {
		clonedPlugin := plugin
		clonedPlugin.Permissions.Capabilities = append([]string(nil), plugin.Permissions.Capabilities...)
		clonedPlugin.Permissions.Network = append([]PluginNetworkTarget(nil), plugin.Permissions.Network...)
		clonedPlugin.Settings = append([]byte(nil), plugin.Settings...)
		clone.Plugins[logicalID] = clonedPlugin
	}
	return clone
}
