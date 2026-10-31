package catalogapi

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCheckCakeMessage(t *testing.T) {
	opt := func(mode string, max int) CakeOptions {
		return CakeOptions{CakeMessageMode: mode, CakeMessageMaxLength: max}
	}
	cases := []struct {
		name    string
		opts    CakeOptions
		msg     string
		require bool
		want    error
	}{
		{"off and empty", opt(OptionOff, 40), "", true, nil},
		{"empty mode reads as off", opt("", 0), "Hi", true, ErrCakeMessageNotAllowed},
		{"off refuses a message", opt(OptionOff, 40), "Hi", true, ErrCakeMessageNotAllowed},
		{"optional empty", opt(OptionOptional, 40), "   ", true, nil},
		{"required empty", opt(OptionRequired, 40), "  ", true, ErrCakeMessageRequired},
		{"required empty, staff", opt(OptionRequired, 40), "", false, nil},
		{"required filled", opt(OptionRequired, 40), "Happy Birthday", true, nil},
		{"at the limit", opt(OptionOptional, 5), "12345", true, nil},
		{"over the limit", opt(OptionOptional, 5), "123456", true, ErrCakeMessageTooLong},
		// Runes, not bytes: five accented letters are five characters.
		{"runes not bytes", opt(OptionOptional, 5), "ééééé", true, nil},
		{"emoji counted once", opt(OptionOptional, 3), "Hi🎂", true, nil},
		{"zero limit is the default 40", opt(OptionOptional, 0), strings.Repeat("a", 41), true, ErrCakeMessageTooLong},
		{"trimmed before counting", opt(OptionOptional, 3), "  abc  ", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.opts.CheckCakeMessage(c.msg, c.require)
			if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

func TestNormaliseProductCode(t *testing.T) {
	if code, ok := NormaliseProductCode("  wpc-040 "); !ok || code != "WPC-040" {
		t.Fatalf("got %q %v", code, ok)
	}
	if code, ok := NormaliseProductCode("   "); !ok || code != "" {
		t.Fatalf("blank: got %q %v", code, ok)
	}
	if _, ok := NormaliseProductCode(strings.Repeat("A", 33)); ok {
		t.Fatal("33 characters accepted")
	}
	if _, ok := NormaliseProductCode(strings.Repeat("A", 32)); !ok {
		t.Fatal("32 characters refused")
	}
	if _, ok := NormaliseProductCode("A\tB"); ok {
		t.Fatal("control character accepted")
	}
}

func TestNormaliseColoursAndSponges(t *testing.T) {
	got, bad := NormaliseColours([]string{" pink", "PINK", "multicolor", "", "Teal"})
	if want := []string{"Pink", "Multicolour"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("colours = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(bad, []string{"Teal"}) {
		t.Fatalf("invalid = %v", bad)
	}
	got, bad = NormaliseSponges([]string{"red  velvet", "black forest", "Vanilla"})
	if want := []string{"Red Velvet", "Black Forest", "Vanilla"}; !reflect.DeepEqual(got, want) || len(bad) != 0 {
		t.Fatalf("sponges = %v (bad %v), want %v", got, bad, want)
	}
	if got, _ := NormaliseColours(nil); got == nil {
		t.Fatal("nil in should give an empty, non-nil list")
	}
}

func TestNormalisePresets(t *testing.T) {
	got := NormalisePresets([]string{" Happy Birthday ", "happy birthday", "", "Congratulations"})
	if want := []string{"Happy Birthday", "Congratulations"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
