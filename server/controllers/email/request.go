package email

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const (
	maxEmailJSONRequestBytes int64 = 64 << 10
	maxEmailOperationIDs           = 500
)

func decodeEmailJSONRequest(w http.ResponseWriter, req *http.Request, target any, maxBytes int64) error {
	req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
