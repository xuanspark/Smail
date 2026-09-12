package migration

import (
	"regexp"
	"strings"
)

func DefaultMigrations() []Migration {
	return []Migration{
		WithCompatibleChecksums(
			DefineForDialects(1, "create_core_schema", coreSchemaStatements()),
			"57b378b273f243bae44e92cad161f297248e5226e96fbd7905815aa8604146bc",
		),
		DefineCustom(2, "expand_user_security", "expand_user_security_v1", applyUserSecuritySchema),
		DefineForDialects(3, "create_security_email_tables", securityEmailStatements()),
		DefineForDialects(4, "create_account_recovery_tables", credentialStatements()),
		DefineForDialects(5, "create_verification_operations_tables", verificationOperationsStatements()),
		DefineCustom(6, "expand_verification_retention", "expand_verification_retention_v1", applyVerificationRetentionSchema),
		DefineForDialects(7, "create_webauthn_credentials", webAuthnCredentialStatements()),
		DefineCustom(8, "mark_webauthn_discoverability", "mark_webauthn_discoverability_v1", applyWebAuthnDiscoverabilitySchema),
		DefineForDialects(9, "create_totp_authenticator", totpAuthenticatorStatements()),
		DefineCustom(10, "expand_user_lifecycle_security", "expand_user_lifecycle_security_v1", applyUserLifecycleSchema),
		DefineForDialects(11, "create_plugin_configuration_tables", pluginSettingsStatements()),
		DefineCustom(12, "expand_user_email_login_addresses", "expand_user_email_login_addresses_v1", applyUserEmailAddressSchema),
		DefineForDialects(13, "create_protocol_credentials", protocolCredentialStatements()),
		DefineCustom(14, "expand_imap_deleted_flag", "expand_imap_deleted_flag_v1", applyIMAPDeletedFlagSchema),
		DefineCustom(15, "expand_security_setup", "expand_security_setup_v1", applySecuritySetupSchema),
		DefineCustom(16, "expand_user_security_preferences", "expand_user_security_preferences_v1", applyUserSecurityPreferencesSchema),
		DefineForDialects(17, "create_managed_logs", logStatements()),
		DefineCustom(18, "expand_operation_log_actor_role", "expand_operation_log_actor_role_v1", applyOperationLogActorRoleSchema),
		DefineForDialects(19, "create_user_contacts_and_trusted_senders", userContactStatements()),
		DefineCustom(20, "expand_protocol_credential_ciphertext", "expand_protocol_credential_ciphertext_v1", applyProtocolCredentialCiphertextSchema),
	}
}

var sqliteIndexDefinitionPattern = regexp.MustCompile(`(?i)^CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`)

// ManagedSQLiteIndexNames 返回版本化迁移允许存在的 SQLite 索引名称。
// 自动生成的 sqlite_autoindex_* 不包含在列表中，由 SQLite 自身维护。
func ManagedSQLiteIndexNames() map[string]struct{} {
	managed := make(map[string]struct{})
	for _, item := range DefaultMigrations() {
		for _, statement := range item.StatementsFor(DialectSQLite) {
			matches := sqliteIndexDefinitionPattern.FindStringSubmatch(strings.TrimSpace(statement))
			if len(matches) == 2 {
				managed[matches[1]] = struct{}{}
			}
		}
	}
	for _, name := range []string{
		"uq_user_email_address_canonical",
		"idx_user_email_user_primary",
		"idx_verification_event_expires_at",
	} {
		managed[name] = struct{}{}
	}
	return managed
}
