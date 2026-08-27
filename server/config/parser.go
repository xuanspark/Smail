package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrConfigTooLarge  = errors.New("configuration is too large")
	ErrConfigBOM       = errors.New("configuration must not contain BOM")
	ErrConfigDuplicate = errors.New("configuration contains duplicate field")
	ErrConfigTrailing  = errors.New("configuration contains trailing data")
	ErrConfigDepth     = errors.New("configuration nesting is too deep")
)

const maxConfigDepth = 32

var (
	rawMessageType = reflect.TypeOf(json.RawMessage{})
	timeType       = reflect.TypeOf(time.Time{})
)

func ParseStrict(data []byte, target any) error {
	if len(data) == 0 || len(data) > maxConfigFileSize {
		return ErrConfigTooLarge
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return ErrConfigBOM
	}
	if !utf8.Valid(data) {
		return errors.New("configuration is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil {
		return errors.New("configuration is not valid JSON")
	}
	if err := scanJSON(decoder, first, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrConfigTrailing
	}
	if err := validateExactFields(data, target); err != nil {
		return errors.New("configuration fields are invalid")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("configuration fields are invalid")
	}
	return nil
}

func scanJSON(decoder *json.Decoder, token json.Token, depth int) error {
	if depth > maxConfigDepth {
		return ErrConfigDepth
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return errors.New("configuration object is invalid")
			}
			key, ok := keyToken.(string)
			if !ok || strings.TrimSpace(key) == "" {
				return errors.New("configuration field name is invalid")
			}
			if _, exists := seen[key]; exists {
				return ErrConfigDuplicate
			}
			seen[key] = struct{}{}
			value, err := decoder.Token()
			if err != nil {
				return errors.New("configuration value is invalid")
			}
			if err := scanJSON(decoder, value, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return errors.New("configuration object is invalid")
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return errors.New("configuration array is invalid")
			}
			if err := scanJSON(decoder, value, depth+1); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return errors.New("configuration array is invalid")
		}
	default:
		return fmt.Errorf("configuration delimiter is invalid")
	}
	return nil
}

func validateExactFields(data []byte, target any) error {
	targetType := reflect.TypeOf(target)
	if targetType == nil || targetType.Kind() != reflect.Pointer || targetType.Elem().Kind() == reflect.Invalid {
		return errors.New("invalid target")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return validateJSONShape(value, targetType.Elem())
}

func validateJSONShape(value any, targetType reflect.Type) error {
	for targetType.Kind() == reflect.Pointer {
		targetType = targetType.Elem()
	}
	if targetType == rawMessageType || targetType == timeType || targetType.Kind() == reflect.Interface {
		return nil
	}
	switch targetType.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := jsonFields(targetType)
		for key, item := range object {
			fieldType, exists := fields[key]
			if !exists {
				return errors.New("unknown field")
			}
			if err := validateJSONShape(item, fieldType); err != nil {
				return err
			}
		}
	case reflect.Map:
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for _, item := range object {
			if err := validateJSONShape(item, targetType.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if targetType == rawMessageType {
			return nil
		}
		items, ok := value.([]any)
		if !ok {
			return nil
		}
		for _, item := range items {
			if err := validateJSONShape(item, targetType.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonFields(targetType reflect.Type) map[string]reflect.Type {
	result := make(map[string]reflect.Type)
	for i := 0; i < targetType.NumField(); i++ {
		field := targetType.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			if field.Anonymous {
				for nestedName, nestedType := range jsonFields(field.Type) {
					result[nestedName] = nestedType
				}
				continue
			}
			name = field.Name
		}
		result[name] = field.Type
	}
	return result
}
