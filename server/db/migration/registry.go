package migration

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
	}
}
