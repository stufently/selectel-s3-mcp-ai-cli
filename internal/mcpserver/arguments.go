package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Validate the original stream before unmarshalling maps loses duplicate keys.
// Struct decoding matches keys without regard to case, including nested objects.
func validateArgumentKeys(raw json.RawMessage) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := validateJSONValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func validateJSONValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		var keys []string
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("expected object key")
			}
			for _, previous := range keys {
				if strings.EqualFold(previous, key) {
					return fmt.Errorf("case-colliding keys %q and %q", previous, key)
				}
			}
			keys = append(keys, key)
			if err := validateJSONValue(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := validateJSONValue(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
	_, err = d.Token()
	return err
}
