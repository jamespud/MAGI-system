package a2aapp

import (
	"strings"
	"testing"
	"time"
)

func TestCursorCodecRoundTrip(t *testing.T) {
	c := CursorCodec{MaxPageSize: 100}
	want := TaskCursor{CreatedAt: time.Date(2026, 8, 25, 1, 2, 3, 400, time.UTC), ID: "case-1"}
	encoded, err := c.Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, "=") {
		t.Fatalf("cursor has padding: %q", encoded)
	}
	got, err := c.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
}

func TestCursorCodecEncodeNormalizesNonUTC(t *testing.T) {
	c := CursorCodec{MaxPageSize: 100}
	local := time.Date(2026, 8, 25, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))
	encoded, err := c.Encode(TaskCursor{CreatedAt: local, ID: "case-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("normalized timestamp = %+v", got.CreatedAt)
	}
}

func TestCursorCodecRejectsMalformedValues(t *testing.T) {
	c := CursorCodec{MaxPageSize: 100}
	valid, err := c.Encode(TaskCursor{CreatedAt: time.Now().UTC(), ID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{valid + "=", "not-json", "eyJ2IjoyLCJjcmVhdGVkQXQiOiIyMDI2LTA4LTI1VDAxOjAyOjAzWiIsImlkIjoiaWQifQ", "eyJ2IjoxLCJjcmVhdGVkQXQiOiIyMDI2LTA4LTI1VDAxOjAyOjAzWiIsImlkIjoiIn0", "eyJ2IjoxLCJjcmVhdGVkQXQiOiIyMDI2LTA4LTI1VDAxOjAyOjAzKzAxOjAwIiwiaWQiOiJpZCJ9"} {
		if _, err := c.Decode(value); err == nil {
			t.Fatalf("Decode(%q) succeeded", value)
		}
	}
}

func TestCursorCodecPageSize(t *testing.T) {
	c := CursorCodec{MaxPageSize: 100}
	for _, tt := range []struct{ in, want int }{{0, 50}, {-1, 50}, {3, 3}, {99, 99}} {
		if got := c.PageSize(tt.in); got != tt.want {
			t.Fatalf("PageSize(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
	if got := (CursorCodec{MaxPageSize: 7}).PageSize(99); got != 7 {
		t.Fatalf("PageSize above maximum = %d, want 7", got)
	}
}
