package imap_server

import (
	"fmt"
	"math"

	"github.com/emersion/go-imap/v2"
	"github.com/spf13/cast"
)

func imapUint32(value int) (uint32, error) {
	if value < 0 || uint64(value) > math.MaxUint32 {
		return 0, fmt.Errorf("IMAP number out of range")
	}
	return cast.ToUint32(value), nil
}

func imapUID(value int) (imap.UID, error) {
	number, err := imapUint32(value)
	if err != nil || number == 0 {
		return 0, fmt.Errorf("IMAP UID out of range")
	}
	return imap.UID(number), nil
}
