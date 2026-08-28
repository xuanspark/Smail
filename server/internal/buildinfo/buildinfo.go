package buildinfo

import "strings"

const ProtocolVersion = 1

var (
	Version   = "dev"
	GitHash   = "unknown"
	BuildTime = "unknown"
	GoVersion = "unknown"
)

type Info struct {
	Version         string
	GitHash         string
	BuildTime       string
	GoVersion       string
	ProtocolVersion int
}

func Current() Info {
	return Info{
		Version:         valueOrDefault(Version, "dev"),
		GitHash:         valueOrDefault(GitHash, "unknown"),
		BuildTime:       valueOrDefault(BuildTime, "unknown"),
		GoVersion:       valueOrDefault(GoVersion, "unknown"),
		ProtocolVersion: ProtocolVersion,
	}
}

func ValidatePair(mainInfo, adminInfo Info) error {
	if strings.TrimSpace(mainInfo.Version) == "" || strings.TrimSpace(adminInfo.Version) == "" {
		return ErrVersionMissing
	}
	if mainInfo.Version != adminInfo.Version {
		return ErrVersionMismatch
	}
	if mainInfo.ProtocolVersion != ProtocolVersion || adminInfo.ProtocolVersion != ProtocolVersion {
		return ErrProtocolMismatch
	}
	if isKnownValue(mainInfo.GitHash) && isKnownValue(adminInfo.GitHash) && mainInfo.GitHash != adminInfo.GitHash {
		return ErrBuildMismatch
	}
	return nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isKnownValue(value string) bool {
	return strings.TrimSpace(value) != "" && value != "unknown" && value != "dev"
}
