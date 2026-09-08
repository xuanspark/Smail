package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	ErrConfigRootMissing        = errors.New("configuration root is missing")
	ErrConfigSectionMissing     = errors.New("configuration section is missing")
	ErrConfigDigestMismatch     = errors.New("configuration section digest mismatch")
	ErrConfigGenerationMismatch = errors.New("configuration generation mismatch")
	ErrConfigRecoveryRequired   = errors.New("configuration recovery is required")
)

type ConfigLoaderOptions struct {
	Validation RuntimeValidationOptions
}

type ConfigLoader struct {
	root    string
	options ConfigLoaderOptions
}

func NewConfigLoader(root string, options ConfigLoaderOptions) *ConfigLoader {
	return &ConfigLoader{root: root, options: options}
}

func (l *ConfigLoader) Load() (RuntimeConfig, error) {
	if l == nil || l.root == "" {
		return RuntimeConfig{}, ErrConfigRootMissing
	}
	directory, err := secureManagerConfigDirectory(l.root)
	if err != nil {
		return RuntimeConfig{}, err
	}
	rootData, err := readManagerConfigFile(directory, "config.json")
	if err != nil {
		if os.IsNotExist(err) {
			return RuntimeConfig{}, ErrConfigRootMissing
		}
		return RuntimeConfig{}, err
	}
	var root RootConfig
	if err := ParseStrict(rootData, &root); err != nil {
		return RuntimeConfig{}, fmt.Errorf("load root configuration: %w", err)
	}
	root = normalizeRootConfig(root)
	if err := ValidateSectionReferences(root.Sections, root.Generation); err != nil {
		return RuntimeConfig{}, err
	}
	runtime := RuntimeConfig{
		Generation: root.Generation,
		SetupState: root.SetupState,
		Root:       cloneRootConfig(root),
		LoadedAt:   time.Now().UTC(),
	}
	sectionData := make(map[Section][]byte, len(requiredPartitionSections))
	for _, section := range requiredPartitionSections {
		file, _ := FixedSectionFile(section)
		data, readErr := readManagerConfigFile(directory, file)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return RuntimeConfig{}, fmt.Errorf("%w: %s", ErrConfigSectionMissing, file)
			}
			return RuntimeConfig{}, readErr
		}
		reference := root.Sections[section]
		if SectionDigest(data) != reference.SHA256 {
			return RuntimeConfig{}, fmt.Errorf("%w: %s", ErrConfigDigestMismatch, file)
		}
		sectionData[section] = data
	}
	if err := ParseStrict(sectionData[SectionSMTP], &runtime.SMTP); err != nil {
		return RuntimeConfig{}, fmt.Errorf("load SMTP section: %w", err)
	}
	if err := ParseStrict(sectionData[SectionEmail], &runtime.Email); err != nil {
		return RuntimeConfig{}, fmt.Errorf("load email section: %w", err)
	}
	if err := ParseStrict(sectionData[SectionSSL], &runtime.SSL); err != nil {
		return RuntimeConfig{}, fmt.Errorf("load SSL section: %w", err)
	}
	if err := ParseStrict(sectionData[SectionPlugins], &runtime.Plugins); err != nil {
		return RuntimeConfig{}, fmt.Errorf("load plugin section: %w", err)
	}
	if err := ValidateRuntimeConfig(runtime, l.options.Validation); err != nil {
		return RuntimeConfig{}, err
	}
	runtime.SSL.TLS = normalizeSSLDataReferences(runtime.SSL.TLS)
	return cloneRuntimeConfig(runtime), nil
}

func (l *ConfigLoader) ConfigDirectory() (string, error) {
	if l == nil || l.root == "" {
		return "", ErrConfigRootMissing
	}
	return secureManagerConfigDirectory(l.root)
}

func readConfigSection(root string, section Section) ([]byte, error) {
	file, ok := FixedSectionFile(section)
	if !ok || section == SectionRoot {
		return nil, ErrInvalidSection
	}
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		return nil, err
	}
	return readManagerConfigFile(directory, filepath.Base(file))
}
