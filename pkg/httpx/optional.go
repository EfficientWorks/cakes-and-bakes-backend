package httpx

import (
	"bytes"
	"encoding/json"
	"strings"
)

// OptionalString tells three JSON cases apart that a *string cannot:
//
//	key absent        -> Set=false            -> leave the stored value alone
//	"field": null     -> Set=true,  Value=nil -> clear it
//	"field": ""       -> Set=true,  Value=nil -> clear it
//	"field": "abc"    -> Set=true,  Value=abc -> write it
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
		o.Value = nil
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

// Ptr maps the three cases onto a *string for a domain input where nil
// means "keep" and "" means "clear": absent -> nil, null/"" -> &"", value -> &value.
func (o OptionalString) Ptr() *string {
	if !o.Set {
		return nil
	}
	if o.Value == nil {
		empty := ""
		return &empty
	}
	return o.Value
}
