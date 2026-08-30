package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	fileutil "git.pcux.cn/xuans/Smail/utils/file"
)

type TransactionPhase string

const (
	TransactionPrepared          TransactionPhase = "prepared"
	TransactionSectionsCommitted TransactionPhase = "sections_committed"
	TransactionRootCommitted     TransactionPhase = "root_committed"
	TransactionVerified          TransactionPhase = "verified"
	TransactionRolledBack        TransactionPhase = "rolled_back"
)

type TransactionManifest struct {
	ID             string            `json:"id"`
	FromGeneration uint64            `json:"fromGeneration"`
	ToGeneration   uint64            `json:"toGeneration"`
	Sections       []Section         `json:"sections"`
	OriginalFiles  map[string]string `json:"originalFiles"`
	CandidateFiles map[string]string `json:"candidateFiles"`
	Phase          TransactionPhase  `json:"phase"`
	CreatedAt      time.Time         `json:"createdAt"`
}

type TransactionHooks struct {
	BeforePhase func(TransactionPhase) error
}

type TransactionStore struct {
	root       string
	validation RuntimeValidationOptions
	hooks      TransactionHooks
}

var transactionIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

const (
	configBackupInterval = 7 * 24 * time.Hour
	configBackupMaxAge   = 30 * 24 * time.Hour
	configBackupLimit    = 4
)

var (
	ErrTransactionInvalid = errors.New("invalid configuration transaction")
	ErrTransactionPhase   = errors.New("invalid configuration transaction phase")
)

func NewTransactionStore(root string, validation RuntimeValidationOptions) *TransactionStore {
	return &TransactionStore{root: root, validation: validation}
}

func (s *TransactionStore) WithHooks(hooks TransactionHooks) *TransactionStore {
	if s != nil {
		s.hooks = hooks
	}
	return s
}

func (s *TransactionStore) Commit(current, candidate RuntimeConfig) error {
	if s == nil || s.root == "" {
		return ErrManagerStorage
	}
	if current.Generation == 0 || candidate.Generation != current.Generation+1 {
		return ErrGenerationConflict
	}
	if err := ValidateRuntimeConfig(candidate, s.validation); err != nil {
		return err
	}
	files, err := marshalRuntimeFiles(candidate)
	if err != nil {
		return err
	}
	currentFiles, err := marshalRuntimeFiles(current)
	if err != nil {
		return err
	}
	directory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return err
	}
	transactions, err := secureTransactionDirectory(directory)
	if err != nil {
		return err
	}
	id, err := newTransactionID()
	if err != nil {
		return err
	}
	transactionDirectory := filepath.Join(transactions, id)
	candidateDirectory := filepath.Join(transactionDirectory, "candidate")
	originalDirectory := filepath.Join(transactionDirectory, "original")
	manifestWritten := false
	defer func() {
		if !manifestWritten {
			_ = os.RemoveAll(transactionDirectory)
		}
	}()
	if err := os.MkdirAll(candidateDirectory, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(originalDirectory, 0700); err != nil {
		return err
	}
	if err := validateTransactionDirectory(transactionDirectory); err != nil {
		return err
	}
	for name, data := range currentFiles {
		if err := atomicfile.Write(filepath.Join(originalDirectory, name), data, 0600); err != nil {
			return err
		}
	}
	for name, data := range files {
		if err := atomicfile.Write(filepath.Join(candidateDirectory, name), data, 0600); err != nil {
			return err
		}
	}
	manifest := TransactionManifest{
		ID:             id,
		FromGeneration: current.Generation,
		ToGeneration:   candidate.Generation,
		Sections:       append([]Section(nil), requiredPartitionSections...),
		OriginalFiles:  digestFiles(currentFiles),
		CandidateFiles: digestFiles(files),
		Phase:          TransactionPrepared,
		CreatedAt:      time.Now().UTC(),
	}
	if err := writeTransactionManifest(transactionDirectory, manifest); err != nil {
		return err
	}
	manifestWritten = true
	if err := s.beforePhase(manifest.Phase); err != nil {
		return err
	}
	for _, section := range requiredPartitionSections {
		name, _ := FixedSectionFile(section)
		if err := replaceLiveConfigFile(directory, filepath.Join(candidateDirectory, name), name); err != nil {
			return err
		}
	}
	manifest.Phase = TransactionSectionsCommitted
	if err := writeTransactionManifest(transactionDirectory, manifest); err != nil {
		return err
	}
	if err := s.beforePhase(manifest.Phase); err != nil {
		return err
	}
	if err := replaceLiveConfigFile(directory, filepath.Join(candidateDirectory, "config.json"), "config.json"); err != nil {
		return err
	}
	manifest.Phase = TransactionRootCommitted
	if err := writeTransactionManifest(transactionDirectory, manifest); err != nil {
		return err
	}
	if err := s.beforePhase(manifest.Phase); err != nil {
		return err
	}
	loader := NewConfigLoader(s.root, ConfigLoaderOptions{Validation: s.validation})
	loaded, err := loader.Load()
	if err != nil || loaded.Generation != candidate.Generation {
		if err != nil {
			return fmt.Errorf("verify committed configuration: %w", err)
		}
		return ErrConfigGenerationMismatch
	}
	manifest.Phase = TransactionVerified
	if err := writeTransactionManifest(transactionDirectory, manifest); err != nil {
		return err
	}
	if err := s.beforePhase(manifest.Phase); err != nil {
		return err
	}
	if err := os.RemoveAll(transactionDirectory); err != nil {
		return err
	}
	manifestWritten = false
	return nil
}

func (s *TransactionStore) Recover() error {
	if s == nil || s.root == "" {
		return ErrManagerStorage
	}
	directory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return err
	}
	transactions, err := secureTransactionDirectory(directory)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(transactions)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !transactionIDPattern.MatchString(entry.Name()) {
			return ErrTransactionInvalid
		}
		transactionDirectory := filepath.Join(transactions, entry.Name())
		if err := validateTransactionDirectory(transactionDirectory); err != nil {
			return err
		}
		manifest, err := readTransactionManifest(transactionDirectory)
		if err != nil || manifest.ID != entry.Name() {
			return ErrTransactionInvalid
		}
		if err := validateTransactionFiles(transactionDirectory, "candidate", manifest.CandidateFiles); err != nil {
			return err
		}
		if err := validateTransactionFiles(transactionDirectory, "original", manifest.OriginalFiles); err != nil {
			return err
		}
		switch manifest.Phase {
		case TransactionPrepared, TransactionSectionsCommitted:
			if err := restoreTransactionOriginal(directory, transactionDirectory, manifest); err != nil {
				return err
			}
			manifest.Phase = TransactionRolledBack
			if err := writeTransactionManifest(transactionDirectory, manifest); err != nil {
				return err
			}
			if err := os.RemoveAll(transactionDirectory); err != nil {
				return err
			}
		case TransactionRootCommitted, TransactionVerified:
			loader := NewConfigLoader(s.root, ConfigLoaderOptions{Validation: s.validation})
			runtime, loadErr := loader.Load()
			if loadErr == nil && runtime.Generation == manifest.ToGeneration {
				if err := os.RemoveAll(transactionDirectory); err != nil {
					return err
				}
				continue
			}
			if err := restoreTransactionOriginal(directory, transactionDirectory, manifest); err != nil {
				return err
			}
			manifest.Phase = TransactionRolledBack
			if err := writeTransactionManifest(transactionDirectory, manifest); err != nil {
				return err
			}
			if err := os.RemoveAll(transactionDirectory); err != nil {
				return err
			}
		case TransactionRolledBack:
			if err := os.RemoveAll(transactionDirectory); err != nil {
				return err
			}
		default:
			return ErrTransactionPhase
		}
	}
	return nil
}

func (s *TransactionStore) beforePhase(phase TransactionPhase) error {
	if s != nil && s.hooks.BeforePhase != nil {
		return s.hooks.BeforePhase(phase)
	}
	return nil
}

func persistRuntimeConfig(root string, current, candidate RuntimeConfig) error {
	store := NewTransactionStore(root, RuntimeValidationOptions{HasSuperAdmin: true})
	return store.Commit(current, candidate)
}

func marshalRuntimeFiles(runtime RuntimeConfig) (map[string][]byte, error) {
	return marshalRuntimeFilesWithPluginSecrets(runtime, false)
}

type persistedPluginInstance struct {
	Enabled     bool              `json:"enabled"`
	Executable  string            `json:"executable"`
	Permissions PluginPermissions `json:"permissions"`
	Settings    json.RawMessage   `json:"settings,omitempty"`
}

type persistedPluginsSection struct {
	Meta    SectionMeta                        `json:"meta"`
	Plugins map[string]persistedPluginInstance `json:"plugins"`
}

func marshalRuntimeFilesWithPluginSecrets(runtime RuntimeConfig, keepPluginSecrets bool) (map[string][]byte, error) {
	runtime = cloneRuntimeConfig(runtime)
	runtime.Root.Generation = runtime.Generation
	runtime.Root.SetupState = runtime.SetupState
	runtime.SMTP.Meta.Generation = runtime.Generation
	runtime.Email.Meta.Generation = runtime.Generation
	runtime.SSL.Meta.Generation = runtime.Generation
	runtime.Plugins.Meta.Generation = runtime.Generation
	sectionBytes := make(map[Section][]byte, len(requiredPartitionSections))
	for _, section := range requiredPartitionSections {
		var value any
		switch section {
		case SectionSMTP:
			value = runtime.SMTP
		case SectionEmail:
			value = runtime.Email
		case SectionSSL:
			value = runtime.SSL
		case SectionPlugins:
			if keepPluginSecrets {
				value = runtime.Plugins
				break
			}
			plugins := persistedPluginsSection{
				Meta:    runtime.Plugins.Meta,
				Plugins: make(map[string]persistedPluginInstance, len(runtime.Plugins.Plugins)),
			}
			for logicalID, plugin := range runtime.Plugins.Plugins {
				plugins.Plugins[logicalID] = persistedPluginInstance{
					Enabled: plugin.Enabled, Executable: plugin.Executable,
					Permissions: plugin.Permissions, Settings: plugin.Settings,
				}
			}
			value = plugins
		default:
			return nil, ErrInvalidSection
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		sectionBytes[section] = data
	}
	references := make(map[Section]SectionRef, len(requiredPartitionSections))
	for _, section := range requiredPartitionSections {
		file, _ := FixedSectionFile(section)
		schema, _ := FixedSectionSchema(section)
		references[section] = SectionRef{File: file, Schema: schema, Generation: runtime.Generation, SHA256: SectionDigest(sectionBytes[section])}
	}
	runtime.Root.Sections = references
	rootData, err := json.Marshal(runtime.Root)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"config.json": rootData}
	for section, data := range sectionBytes {
		file, _ := FixedSectionFile(section)
		files[file] = data
	}
	return files, nil
}

func replaceLiveConfigFile(directory, source, name string) error {
	if filepath.Base(name) != name {
		return ErrTransactionInvalid
	}
	if err := ensureRegularFile(source); err != nil {
		return err
	}
	target := filepath.Join(directory, name)
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ErrManagerStorage
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := fileutil.ReadRegularFile(source, maxConfigFileSize)
	if err != nil {
		return err
	}
	return atomicfile.Write(target, data, 0600)
}

func restoreTransactionOriginal(directory, transactionDirectory string, manifest TransactionManifest) error {
	if err := validateTransactionManifest(manifest); err != nil {
		return err
	}
	if err := validateTransactionFiles(transactionDirectory, "original", manifest.OriginalFiles); err != nil {
		return err
	}
	originalDirectory := filepath.Join(transactionDirectory, "original")
	files := transactionFileNames()
	for _, name := range files {
		source := filepath.Join(originalDirectory, name)
		data, err := fileutil.ReadRegularFile(source, maxConfigFileSize)
		if err != nil {
			return err
		}
		if err := atomicfile.Write(filepath.Join(directory, name), data, 0600); err != nil {
			return err
		}
	}
	return atomicfile.SyncDirectory(directory)
}

func transactionFileNames() []string {
	files := []string{"config.json"}
	for _, section := range requiredPartitionSections {
		name, _ := FixedSectionFile(section)
		files = append(files, name)
	}
	return files
}

func digestFiles(files map[string][]byte) map[string]string {
	digests := make(map[string]string, len(files))
	for name, data := range files {
		digests[name] = SectionDigest(data)
	}
	return digests
}

func validFileDigestSet(digests map[string]string) bool {
	if len(digests) != len(transactionFileNames()) {
		return false
	}
	expected := make(map[string]struct{}, len(transactionFileNames()))
	for _, name := range transactionFileNames() {
		expected[name] = struct{}{}
	}
	for name, digest := range digests {
		if _, ok := expected[name]; !ok || !validSHA256(digest) {
			return false
		}
	}
	return true
}

func validateTransactionFiles(transactionDirectory, subdirectory string, expected map[string]string) error {
	if !validFileDigestSet(expected) {
		return ErrTransactionInvalid
	}
	directory := filepath.Join(transactionDirectory, subdirectory)
	if err := ensureDirectory(directory); err != nil {
		return err
	}
	for _, name := range transactionFileNames() {
		path := filepath.Join(directory, name)
		if err := ensureRegularFile(path); err != nil {
			return err
		}
		data, err := fileutil.ReadRegularFile(path, maxConfigFileSize)
		if err != nil {
			return err
		}
		if SectionDigest(data) != expected[name] {
			return ErrConfigDigestMismatch
		}
	}
	return nil
}

func writeTransactionManifest(directory string, manifest TransactionManifest) error {
	if err := validateTransactionManifest(manifest); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(directory, "manifest.json"), data, 0600)
}

func readTransactionManifest(directory string) (TransactionManifest, error) {
	var manifest TransactionManifest
	if err := ensureDirectory(directory); err != nil {
		return manifest, err
	}
	path := filepath.Join(directory, "manifest.json")
	if err := ensureRegularFile(path); err != nil {
		return manifest, err
	}
	data, err := fileutil.ReadRegularFile(path, maxConfigFileSize)
	if err != nil {
		return manifest, err
	}
	if err := ParseStrict(data, &manifest); err != nil {
		return manifest, err
	}
	return manifest, validateTransactionManifest(manifest)
}

func validateTransactionManifest(manifest TransactionManifest) error {
	if !transactionIDPattern.MatchString(manifest.ID) || manifest.FromGeneration == 0 || manifest.ToGeneration != manifest.FromGeneration+1 || manifest.CreatedAt.IsZero() {
		return ErrTransactionInvalid
	}
	if len(manifest.Sections) != len(requiredPartitionSections) || !validFileDigestSet(manifest.OriginalFiles) || !validFileDigestSet(manifest.CandidateFiles) {
		return ErrTransactionInvalid
	}
	seen := make(map[Section]struct{}, len(manifest.Sections))
	for _, section := range manifest.Sections {
		if section == SectionRoot {
			return ErrTransactionInvalid
		}
		if _, ok := FixedSectionFile(section); !ok {
			return ErrTransactionInvalid
		}
		if _, ok := seen[section]; ok {
			return ErrTransactionInvalid
		}
		seen[section] = struct{}{}
	}
	switch manifest.Phase {
	case TransactionPrepared, TransactionSectionsCommitted, TransactionRootCommitted, TransactionVerified, TransactionRolledBack:
		return nil
	default:
		return ErrTransactionPhase
	}
}

func secureTransactionDirectory(configDirectory string) (string, error) {
	directory := filepath.Join(configDirectory, "transaction")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	if err := ensureDirectory(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func validateTransactionDirectory(directory string) error {
	if err := ensureDirectory(directory); err != nil {
		return err
	}
	for _, name := range []string{"candidate", "original"} {
		if err := ensureDirectory(filepath.Join(directory, name)); err != nil {
			return err
		}
	}
	return nil
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrTransactionInvalid
	}
	return nil
}

func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxConfigFileSize {
		return ErrTransactionInvalid
	}
	return nil
}

func newTransactionID() (string, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return strings.ToLower(hex.EncodeToString(token)), nil
}

type ConfigBackupManifest struct {
	ID         string            `json:"id"`
	Generation uint64            `json:"generation"`
	CreatedAt  time.Time         `json:"createdAt"`
	Files      map[string]string `json:"files"`
}

func (s *TransactionStore) CreateBackup(runtime RuntimeConfig) (string, error) {
	return s.createBackupAt(runtime, time.Now().UTC())
}

func (s *TransactionStore) CreateBackupIfDue(runtime RuntimeConfig, now time.Time) (string, bool, error) {
	if s == nil || s.root == "" || runtime.Generation == 0 {
		return "", false, ErrManagerStorage
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	latest, found, err := s.latestBackup()
	if err != nil {
		return "", false, err
	}
	if found && now.Before(latest.CreatedAt.Add(configBackupInterval)) {
		return filepath.Join(s.root, "config", "backups-v2", latest.ID), false, nil
	}
	path, err := s.createBackupAt(runtime, now)
	return path, true, err
}

func (s *TransactionStore) createBackupAt(runtime RuntimeConfig, createdAt time.Time) (string, error) {
	if s == nil || s.root == "" || runtime.Generation == 0 {
		return "", ErrManagerStorage
	}
	if err := ValidateRuntimeConfig(runtime, s.validation); err != nil {
		return "", err
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	} else {
		createdAt = createdAt.UTC()
	}
	files, err := marshalRuntimeFiles(runtime)
	if err != nil {
		return "", err
	}
	configDirectory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return "", err
	}
	backupsDirectory, err := secureConfigBackupDirectory(configDirectory)
	if err != nil {
		return "", err
	}
	id, err := newTransactionID()
	if err != nil {
		return "", err
	}
	temporaryDirectory := filepath.Join(backupsDirectory, "."+id+".tmp")
	finalDirectory := filepath.Join(backupsDirectory, id)
	renamed := false
	defer func() {
		if !renamed {
			_ = os.RemoveAll(temporaryDirectory)
		}
	}()
	if err := os.MkdirAll(filepath.Join(temporaryDirectory, "files"), 0700); err != nil {
		return "", err
	}
	if err := ensureDirectory(filepath.Join(temporaryDirectory, "files")); err != nil {
		return "", err
	}
	manifest := ConfigBackupManifest{ID: id, Generation: runtime.Generation, CreatedAt: createdAt, Files: make(map[string]string, len(files))}
	for name, data := range files {
		manifest.Files[name] = SectionDigest(data)
		if err := atomicfile.Write(filepath.Join(temporaryDirectory, "files", name), data, 0600); err != nil {
			return "", err
		}
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(filepath.Join(temporaryDirectory, "backup.json"), manifestData, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryDirectory, finalDirectory); err != nil {
		return "", err
	}
	renamed = true
	if _, _, err := loadConfigBackup(finalDirectory, s.validation); err != nil {
		_ = os.RemoveAll(finalDirectory)
		return "", err
	}
	if err := cleanupConfigBackups(backupsDirectory, createdAt, s.validation); err != nil {
		return "", err
	}
	return finalDirectory, nil
}

func (s *TransactionStore) latestBackup() (ConfigBackupManifest, bool, error) {
	var latest ConfigBackupManifest
	if s == nil || s.root == "" {
		return latest, false, ErrManagerStorage
	}
	configDirectory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return latest, false, err
	}
	backupsDirectory, err := secureConfigBackupDirectory(configDirectory)
	if err != nil {
		return latest, false, err
	}
	entries, err := os.ReadDir(backupsDirectory)
	if err != nil {
		return latest, false, err
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !transactionIDPattern.MatchString(entry.Name()) {
			return latest, false, ErrTransactionInvalid
		}
		_, manifest, loadErr := loadConfigBackup(filepath.Join(backupsDirectory, entry.Name()), s.validation)
		if loadErr != nil {
			return latest, false, loadErr
		}
		if !found || manifest.CreatedAt.After(latest.CreatedAt) {
			latest = manifest
			found = true
		}
	}
	return latest, found, nil
}

func (s *TransactionStore) LoadBackup(generation uint64) (RuntimeConfig, error) {
	if s == nil || s.root == "" || generation == 0 {
		return RuntimeConfig{}, ErrNoBackup
	}
	configDirectory, err := secureManagerConfigDirectory(s.root)
	if err != nil {
		return RuntimeConfig{}, err
	}
	backupsDirectory, err := secureConfigBackupDirectory(configDirectory)
	if err != nil {
		return RuntimeConfig{}, err
	}
	entries, err := os.ReadDir(backupsDirectory)
	if err != nil {
		return RuntimeConfig{}, err
	}
	var selected RuntimeConfig
	var selectedAt time.Time
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			return RuntimeConfig{}, ErrTransactionInvalid
		}
		if !transactionIDPattern.MatchString(entry.Name()) {
			return RuntimeConfig{}, ErrTransactionInvalid
		}
		runtime, manifest, loadErr := loadConfigBackup(filepath.Join(backupsDirectory, entry.Name()), s.validation)
		if loadErr != nil {
			return RuntimeConfig{}, loadErr
		}
		if manifest.Generation != generation {
			continue
		}
		if !found || manifest.CreatedAt.After(selectedAt) {
			selected = runtime
			selectedAt = manifest.CreatedAt
			found = true
		}
	}
	if !found {
		return RuntimeConfig{}, ErrNoBackup
	}
	return selected, nil
}

func secureConfigBackupDirectory(configDirectory string) (string, error) {
	directory := filepath.Join(configDirectory, "backups-v2")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	if err := ensureDirectory(directory); err != nil {
		return "", err
	}
	return directory, nil
}

func loadConfigBackup(directory string, validation RuntimeValidationOptions) (RuntimeConfig, ConfigBackupManifest, error) {
	var manifest ConfigBackupManifest
	if err := ensureDirectory(directory); err != nil {
		return RuntimeConfig{}, manifest, err
	}
	manifestPath := filepath.Join(directory, "backup.json")
	if err := ensureRegularFile(manifestPath); err != nil {
		return RuntimeConfig{}, manifest, err
	}
	manifestData, err := fileutil.ReadRegularFile(manifestPath, maxConfigFileSize)
	if err != nil {
		return RuntimeConfig{}, manifest, err
	}
	if err := ParseStrict(manifestData, &manifest); err != nil {
		return RuntimeConfig{}, manifest, ErrTransactionInvalid
	}
	if !transactionIDPattern.MatchString(manifest.ID) || filepath.Base(directory) != manifest.ID || manifest.Generation == 0 || manifest.CreatedAt.IsZero() || len(manifest.Files) != len(requiredPartitionSections)+1 {
		return RuntimeConfig{}, manifest, ErrTransactionInvalid
	}
	filesDirectory := filepath.Join(directory, "files")
	if err := ensureDirectory(filesDirectory); err != nil {
		return RuntimeConfig{}, manifest, err
	}
	files := make(map[string][]byte, len(manifest.Files))
	expectedNames := map[string]struct{}{"config.json": {}}
	for _, section := range requiredPartitionSections {
		name, ok := FixedSectionFile(section)
		if !ok {
			return RuntimeConfig{}, manifest, ErrTransactionInvalid
		}
		expectedNames[name] = struct{}{}
	}
	for name, digest := range manifest.Files {
		if _, ok := expectedNames[name]; !ok || !validSHA256(digest) {
			return RuntimeConfig{}, manifest, ErrTransactionInvalid
		}
		path := filepath.Join(filesDirectory, name)
		if err := ensureRegularFile(path); err != nil {
			return RuntimeConfig{}, manifest, err
		}
		data, err := fileutil.ReadRegularFile(path, maxConfigFileSize)
		if err != nil || SectionDigest(data) != digest {
			return RuntimeConfig{}, manifest, ErrConfigDigestMismatch
		}
		files[name] = data
	}
	for name := range expectedNames {
		if _, ok := files[name]; !ok {
			return RuntimeConfig{}, manifest, ErrTransactionInvalid
		}
	}
	runtime, err := parseRuntimeFiles(files, validation)
	if err != nil {
		return RuntimeConfig{}, manifest, err
	}
	if runtime.Generation != manifest.Generation {
		return RuntimeConfig{}, manifest, ErrConfigGenerationMismatch
	}
	return runtime, manifest, nil
}

func parseRuntimeFiles(files map[string][]byte, validation RuntimeValidationOptions) (RuntimeConfig, error) {
	var root RootConfig
	if err := ParseStrict(files["config.json"], &root); err != nil {
		return RuntimeConfig{}, err
	}
	root = normalizeRootConfig(root)
	var smtp SMTPSectionConfig
	var email EmailSectionConfig
	var ssl SSLSectionConfig
	var plugins PluginsSectionConfig
	if err := ParseStrict(files["smtp.json"], &smtp); err != nil {
		return RuntimeConfig{}, err
	}
	if err := ParseStrict(files["email.json"], &email); err != nil {
		return RuntimeConfig{}, err
	}
	if err := ParseStrict(files["ssl.json"], &ssl); err != nil {
		return RuntimeConfig{}, err
	}
	if err := ParseStrict(files["plugins.json"], &plugins); err != nil {
		return RuntimeConfig{}, err
	}
	runtime := RuntimeConfig{Generation: root.Generation, SetupState: root.SetupState, Root: root, SMTP: smtp, Email: email, SSL: ssl, Plugins: plugins}
	if err := ValidateSectionReferences(root.Sections, runtime.Generation); err != nil {
		return RuntimeConfig{}, err
	}
	for _, section := range requiredPartitionSections {
		if err := ValidateSectionMeta(section, sectionMeta(runtime, section), runtime.Generation); err != nil {
			return RuntimeConfig{}, err
		}
	}
	if err := ValidateRuntimeConfig(runtime, validation); err != nil {
		return RuntimeConfig{}, err
	}
	return runtime, nil
}

func sectionMeta(runtime RuntimeConfig, section Section) SectionMeta {
	switch section {
	case SectionSMTP:
		return runtime.SMTP.Meta
	case SectionEmail:
		return runtime.Email.Meta
	case SectionSSL:
		return runtime.SSL.Meta
	case SectionPlugins:
		return runtime.Plugins.Meta
	default:
		return SectionMeta{}
	}
}

func cleanupConfigBackups(directory string, now time.Time, validation RuntimeValidationOptions) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	type backupEntry struct {
		path      string
		createdAt time.Time
	}
	backups := make([]backupEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || !transactionIDPattern.MatchString(entry.Name()) {
			return ErrTransactionInvalid
		}
		_, manifest, err := loadConfigBackup(filepath.Join(directory, entry.Name()), validation)
		if err != nil {
			return err
		}
		backups = append(backups, backupEntry{path: filepath.Join(directory, entry.Name()), createdAt: manifest.CreatedAt})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].createdAt.Equal(backups[j].createdAt) {
			return backups[i].path > backups[j].path
		}
		return backups[i].createdAt.After(backups[j].createdAt)
	})
	for index := len(backups) - 1; index >= 1; index-- {
		if index < configBackupLimit && now.Sub(backups[index].createdAt) <= configBackupMaxAge {
			continue
		}
		if err := os.RemoveAll(backups[index].path); err != nil {
			return err
		}
	}
	return nil
}
