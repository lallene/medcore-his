package scheduling

import (
	"os"
	"testing"
)

func TestDefaultTimezoneUTC(t *testing.T) {
	_ = os.Unsetenv(EnvTimezone)
	if err := LoadLocationFromEnv(); err != nil {
		t.Fatal(err)
	}
	if LocationName() != "UTC" {
		t.Fatalf("want UTC got %s", LocationName())
	}
}

func TestSetIANALocation(t *testing.T) {
	if err := SetLocation("Europe/Paris"); err != nil {
		t.Fatal(err)
	}
	if LocationName() != "Europe/Paris" {
		t.Fatalf("got %s", LocationName())
	}
	_ = SetLocation(DefaultTimezone)
}

func TestInvalidTimezone(t *testing.T) {
	if err := SetLocation("Not/AZone"); err == nil {
		t.Fatal("expected error")
	}
}
