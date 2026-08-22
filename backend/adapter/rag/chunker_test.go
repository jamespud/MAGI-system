package rag

import (
	"strings"
	"testing"
)

func TestChunkNestedHierarchy(t *testing.T) {
	// With charsPerToken=1: 300 tokens = 300 chars. Each 50-char sentence = one
	// 300-block; one 900-block = 3 sentences; one 1800-block = 6 sentences = 2x900.
	tc := RuneTokenCounter{CharsPerToken: 1}
	ch := NewChunker(tc, ChunkLevels{L1800: 300, L900: 150, L300: 50})
	sentences := make([]string, 6)
	for i := 0; i < 6; i++ {
		sentences[i] = padTo("sentence"+itoa(i), 49) + "."
	}
	text := joinSentences(sentences)
	doc := ch.Chunk(text, "case_memory", "case-1")

	if len(doc.Chunks1800) != 1 {
		t.Fatalf("chunks1800 = %d, want 1", len(doc.Chunks1800))
	}
	if len(doc.Chunks900) != 2 {
		t.Fatalf("chunks900 = %d, want 2 (1800 = 2x900)", len(doc.Chunks900))
	}
	if len(doc.Chunks300) != 6 {
		t.Fatalf("chunks300 = %d, want 6 (1800 = 6x300)", len(doc.Chunks300))
	}
	for _, c := range doc.Chunks900 {
		if c.Parent1800ID != doc.Chunks1800[0].ID {
			t.Errorf("900 parent = %q, want %q", c.Parent1800ID, doc.Chunks1800[0].ID)
		}
	}
	for i, c := range doc.Chunks300 {
		wantParent := doc.Chunks900[i/3].ID
		if c.Parent900ID != wantParent {
			t.Errorf("300[%d] parent = %q, want %q", i, c.Parent900ID, wantParent)
		}
	}
}

func TestChunkDoesNotSplitSentences(t *testing.T) {
	tc := RuneTokenCounter{CharsPerToken: 1}
	ch := NewChunker(tc, ChunkLevels{L1800: 100, L900: 50, L300: 20})
	// A single sentence longer than 300 (20) stays whole in one 300 block.
	text := padTo("onelongword", 60)
	doc := ch.Chunk(text, "case_memory", "case-1")
	if len(doc.Chunks300) != 1 {
		t.Fatalf("chunks300 = %d, want 1 (unsplittable sentence stays whole)", len(doc.Chunks300))
	}
}

func padTo(s string, n int) string {
	for len(s) < n {
		s += "x"
	}
	return s[:n]
}
func itoa(i int) string {
	return string(rune('0' + i))
}
func joinSentences(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s
	}
	return out
}

func TestSplitSentencesChinesePunctuation(t *testing.T) {
	got := splitSentences("句一。句二！句三？句四；句五")
	if len(got) != 5 {
		t.Fatalf("sentences = %d, want 5: %v", len(got), got)
	}
	if got[0] != "句一。" || got[4] != "句五" {
		t.Errorf("sentences = %v", got)
	}
}

func TestSplitSentencesKeepsCommaInSentence(t *testing.T) {
	got := splitSentences("甲，乙。丙。")
	if len(got) != 2 {
		t.Fatalf("sentences = %d, want 2: %v", len(got), got)
	}
}

func TestRuneTokenCounterCJKAdaptive(t *testing.T) {
	c := RuneTokenCounter{CharsPerToken: 4}
	ascii := strings.Repeat("a", 400)
	if n := c.Count(ascii); n != 100 {
		t.Errorf("ascii 400 chars = %d, want 100 (charsPerToken 4)", n)
	}
	chinese := strings.Repeat("中", 400)
	if n := c.Count(chinese); n != 400 {
		t.Errorf("chinese 400 chars = %d, want 400 (charsPerToken ~1)", n)
	}
	mixed := strings.Repeat("中a", 200) // 400 runes, 50% CJK
	if n := c.Count(mixed); n != 160 {
		t.Errorf("mixed 400 runes = %d, want 160 (charsPerToken ~2.5)", n)
	}
}
