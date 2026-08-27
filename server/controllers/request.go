package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxJSONRequestBytes int64 = 64 << 10

func decodeJSONRequest(w http.ResponseWriter, req *http.Request, target any) error {
	req.Body = http.MaxBytesReader(w, req.Body, maxJSONRequestBytes)
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
