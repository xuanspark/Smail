module smail-test-password-check

go 1.26.0

require git.pcux.cn/xuans/Smail v0.0.0

require (
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace git.pcux.cn/xuans/Smail => ../../../backend
