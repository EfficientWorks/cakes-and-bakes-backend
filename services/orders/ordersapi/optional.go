package ordersapi

import (
	"bytes"
	"encoding/json"
	"strings"
)

// OptionalString tells three JSON cases apart that a *string cannot:
//
//	key absent          -> Set=false            -> leave the column alone
//	"customerId": null  -> Set=true,  Value=nil -> clear the column
//	"customerId": "abc" -> Set=true,  Value=abc -> write the column
type OptionalString struct {
	Set   bool
	Value *string
}

func (o *OptionalString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		o.Value = nil // empty string is the same request as null: detach
		return nil
	}
	o.Value = &s
	return nil
}

func (o OptionalString) MarshalJSON() ([]byte, error) {
	if !o.Set || o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Value)
}
