package domain

import (
	"reflect"
	"strings"
	"testing"

	"bakehouse/services/catalog/catalogapi"
)

func TestNormaliseProductOptionsDefaults(t *testing.T) {
	code := "  wpc-040 "
	p := catalogapi.Product{Code: &code, Colours: []string{"pink"}, Sponges: []string{"vanilla"}}
	if err := normaliseProductOptions(&p); err != nil {
		t.Fatal(err)
	}
	if p.Code == nil || *p.Code != "WPC-040" {
		t.Fatalf("code = %v", p.Code)
	}
	if p.CakeMessageMode != catalogapi.OptionOff || p.CakeMessageMaxLength != 40 {
		t.Fatalf("mode/max = %q/%d", p.CakeMessageMode, p.CakeMessageMaxLength)
	}
	if !reflect.DeepEqual(p.CakeMessagePresets, catalogapi.DefaultCakeMessagePresets()) {
		t.Fatalf("presets = %v", p.CakeMessagePresets)
	}
	if !reflect.DeepEqual(p.Colours, []string{"Pink"}) || !reflect.DeepEqual(p.Sponges, []string{"Vanilla"}) {
		t.Fatalf("colours/sponges = %v %v", p.Colours, p.Sponges)
	}
}

func TestNormaliseProductOptionsRejects(t *testing.T) {
	long := strings.Repeat("X", 33)
	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, strings.Repeat("a", i+1))
	}
	cases := map[string]catalogapi.Product{
		"code too long":    {Code: &long},
		"unknown colour":   {Colours: []string{"Teal"}},
		"unknown sponge":   {Sponges: []string{"Carrot"}},
		"bad mode":         {CakeMessageMode: "sometimes"},
		"max over 200":     {CakeMessageMaxLength: 201},
		"max negative":     {CakeMessageMaxLength: -1},
		"preset over max":  {CakeMessageMaxLength: 5, CakeMessagePresets: []string{"Happy Birthday"}},
		"too many presets": {CakeMessagePresets: many},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if err := normaliseProductOptions(&p); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestNormaliseProductOptionsClears(t *testing.T) {
	// A blank code removes it; explicitly empty presets stay empty.
	blank := "  "
	p := catalogapi.Product{Code: &blank, CakeMessagePresets: []string{}, CakeMessageMode: " Required "}
	if err := normaliseProductOptions(&p); err != nil || p.Code != nil {
		t.Fatalf("blank code: err %v code %v", err, p.Code)
	}
	if p.CakeMessagePresets == nil || len(p.CakeMessagePresets) != 0 {
		t.Fatalf("presets = %v", p.CakeMessagePresets)
	}
	if p.CakeMessageMode != catalogapi.OptionRequired {
		t.Fatalf("mode = %q", p.CakeMessageMode)
	}
}
