package buildinfo

import "errors"

var (
	ErrVersionMissing   = errors.New("build version is missing")
	ErrVersionMismatch  = errors.New("main and admin versions do not match")
	ErrProtocolMismatch = errors.New("main and admin protocol versions do not match")
	ErrBuildMismatch    = errors.New("main and admin build identifiers do not match")
)
