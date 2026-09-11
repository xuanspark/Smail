package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

var ErrInvalidLength = errors.New("password length is invalid")

const (
	format          = "argon2id"
	version         = 19
	memory          = 64 * 1024
	iterations      = 3
	parallelism     = 1
	saltLength      = 16
	keyLength       = 32
	maxHashSize     = 255
	minSecretSize   = 1
	minPasswordSize = 8
	maxPasswordSize = 12
)

// ValidateUserPassword 校验登录密码的统一长度边界。
// 长度按 UTF-8 编码后的字节数计算，避免不同入口采用不同规则。
func ValidateUserPassword(plain string) error {
	passwordSize := len([]byte(plain))
	if passwordSize < minPasswordSize || passwordSize > maxPasswordSize {
		return ErrInvalidLength
	}
	return nil
}

// Hash 生成 Argon2id 密码哈希。
func Hash(plain string) (string, error) {
	plainSize := len([]byte(plain))
	if plainSize < minSecretSize {
		return "", ErrInvalidLength
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, iterations, memory, parallelism, keyLength)
	encoded := fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		format,
		version,
		memory,
		iterations,
		parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	if len(encoded) > maxHashSize {
		return "", errors.New("password hash is too long")
	}
	return encoded, nil
}

// Encode 保留统一的哈希生成入口，但不保留旧算法兼容逻辑。
func Encode(plain string) string {
	hash, err := Hash(plain)
	if err != nil {
		return ""
	}
	return hash
}

// HashUserPassword 生成符合登录密码策略的 Argon2id 哈希。
func HashUserPassword(plain string) (string, error) {
	if err := ValidateUserPassword(plain); err != nil {
		return "", err
	}
	return Hash(plain)
}

// Verify 校验 Argon2id 密码哈希，不接受旧算法格式。
func Verify(encoded, plain string) bool {
	if len([]byte(plain)) < minSecretSize {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != format || parts[2] != fmt.Sprintf("v=%d", version) || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, parallelism) {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != saltLength {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != keyLength {
		return false
	}
	actual := argon2.IDKey([]byte(plain), salt, iterations, memory, parallelism, keyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func VerifyDummy(plain string) bool {
	salt := [saltLength]byte{0x53, 0x6d, 0x61, 0x69, 0x6c, 0x2d, 0x61, 0x75, 0x74, 0x68, 0x2d, 0x64, 0x75, 0x6d, 0x6d, 0x79}
	expected := [keyLength]byte{0x91, 0xa4, 0x65, 0x82, 0x31, 0x07, 0x44, 0xc8, 0x7a, 0x22, 0xe1, 0x59, 0x3c, 0x5b, 0x8d, 0xf1, 0xd4, 0x3a, 0x68, 0x9c, 0xee, 0x20, 0x57, 0x61, 0x45, 0x3a, 0x71, 0x10, 0x5b, 0x26, 0xb6, 0xa0}
	actual := argon2.IDKey([]byte(plain), salt[:], iterations, memory, parallelism, keyLength)
	return subtle.ConstantTimeCompare(actual, expected[:]) == 1
}
