package reader

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestMigratedTopicsExportRoundTrip(t *testing.T) {
	dir := legacyStore(t, legacySources)
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data, err := s.Export(t.Context(), User{ID: 2})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeExport(encoded)
	if err != nil {
		t.Fatalf("the app must accept its own export after migrating topics: %v", err)
	}
	if err := s.RestorePersonal(t.Context(), User{ID: 1}, decoded); err != nil {
		t.Fatal(err)
	}
	before, after := feedTags(t, s, 2, false), feedTags(t, s, 1, false)
	for title, tags := range before {
		if !slices.Equal(tags, after[title]) {
			t.Errorf("%s: restored %v, want %v", title, after[title], tags)
		}
	}
}

func TestTagLimitsPermitEveryValidThirteenTagList(t *testing.T) {
	for _, letter := range []string{"a", "é", "界", "\U00010400"} {
		for count := 0; count <= maxSourceTags; count++ {
			var tags []string
			for i := 0; i < count; i++ {
				tags = append(tags, fmt.Sprint(i)+strings.Repeat(letter, 38))
			}
			got, err := ParseTags(strings.Join(tags, ","))
			if err != nil || len(got) != count {
				t.Fatalf("count=%d letter=%s: %v %v", count, letter, got, err)
			}
		}
	}
	for _, bad := range []string{strings.Repeat("x", maxTagBytes+1), "bad\nline", "bad\x00tag", string([]byte{0xff})} {
		if _, err := ParseTags(bad); err == nil {
			t.Errorf("accepted invalid tags %q", bad)
		}
	}
	var tags []string
	for i := 0; i < 14; i++ {
		tags = append(tags, fmt.Sprint(i))
	}
	if _, err := ParseTags(strings.Join(tags, ",")); err == nil {
		t.Fatal("accepted fourteen tags")
	}
	if _, err := withTopic("extra", tags[:13]); err == nil {
		t.Fatal("topic bypassed the tag limit")
	}
}
