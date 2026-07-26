package rag

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// TokenCounter estimates token count for chunking. The default RuneTokenCounter
// approximates tokens as rune count / CharsPerToken (default 4). A real
// tokenizer (tiktoken etc.) can implement this interface later.
type TokenCounter interface {
	Count(s string) int
}

type RuneTokenCounter struct{ CharsPerToken int }

func (r RuneTokenCounter) Count(s string) int {
	if r.CharsPerToken <= 0 {
		r.CharsPerToken = 4
	}
	return len([]rune(s)) / r.CharsPerToken
}

// ChunkLevels holds the token targets for the three hierarchy levels.
type ChunkLevels struct{ L1800, L900, L300 int }

// ChunkBlock is a single chunk at any level.
type ChunkBlock struct {
	ID           string
	Parent900ID  string // set for 300-level blocks
	Parent1800ID string // set for 900-level blocks
	Source       string
	SourceRef    string
	Content      string
	TokenCount   int
	Seq          int
}

// ChunkedDoc is the result of chunking a document into the 3-level hierarchy.
type ChunkedDoc struct {
	Chunks1800 []ChunkBlock
	Chunks900  []ChunkBlock
	Chunks300  []ChunkBlock
}

type Chunker struct {
	tc     TokenCounter
	levels ChunkLevels
}

func NewChunker(tc TokenCounter, levels ChunkLevels) *Chunker {
	if levels.L1800 == 0 {
		levels = ChunkLevels{L1800: 1800, L900: 900, L300: 300}
	}
	return &Chunker{tc: tc, levels: levels}
}

// Chunk splits text into 1800 -> 900 -> 300 nested blocks by construction.
// Parent IDs are assigned structurally: each 900 knows its 1800; each 300 its 900.
func (c *Chunker) Chunk(text, source, sourceRef string) ChunkedDoc {
	doc := ChunkedDoc{}
	sentences := splitSentences(text)

	// Level 1800: greedy sentence packing to ~L1800 tokens.
	groups1800 := packSentences(sentences, c.levels.L1800, c.tc)
	for gi, g := range groups1800 {
		content := g
		id := chunkID(sourceRef, "1800", gi, 0)
		doc.Chunks1800 = append(doc.Chunks1800, ChunkBlock{
			ID: id, Parent1800ID: "", Source: source, SourceRef: sourceRef,
			Content: content, TokenCount: c.tc.Count(content), Seq: gi,
		})
		// Level 900: split this 1800 into ~2 x L900 at sentence boundary.
		sub900 := packSentences(splitSentences(content), c.levels.L900, c.tc)
		for si, s := range sub900 {
			cid := chunkID(sourceRef, "900", gi, si)
			doc.Chunks900 = append(doc.Chunks900, ChunkBlock{
				ID: cid, Parent1800ID: id, Source: source, SourceRef: sourceRef,
				Content: s, TokenCount: c.tc.Count(s), Seq: si,
			})
			// Level 300: split this 900 into ~3 x L300 at sentence boundary.
			sub300 := packSentences(splitSentences(s), c.levels.L300, c.tc)
			for ti, tt := range sub300 {
				tid := chunkID(sourceRef, "300", gi*100+si*10, ti)
				doc.Chunks300 = append(doc.Chunks300, ChunkBlock{
					ID: tid, Parent900ID: cid, Source: source, SourceRef: sourceRef,
					Content: tt, TokenCount: c.tc.Count(tt), Seq: ti,
				})
			}
		}
	}
	return doc
}

// packSentences greedily packs sentences into blocks of <= target tokens.
// A single sentence exceeding target stays whole (unsplittable).
func packSentences(sentences []string, target int, tc TokenCounter) []string {
	if len(sentences) == 0 {
		return nil
	}
	var blocks []string
	cur := sentences[0]
	curTokens := tc.Count(cur)
	for _, s := range sentences[1:] {
		st := tc.Count(s)
		if curTokens+st <= target || curTokens == 0 {
			cur += s
			curTokens += st
		} else {
			blocks = append(blocks, cur)
			cur = s
			curTokens = st
		}
	}
	blocks = append(blocks, cur)
	return blocks
}

// splitSentences splits text on sentence-end punctuation, keeping the punctuation.
func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var out []string
	cur := strings.Builder{}
	for _, r := range text {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	res := out[:0]
	for _, s := range out {
		if strings.TrimSpace(s) != "" {
			res = append(res, s)
		}
	}
	if len(res) == 0 {
		res = []string{text}
	}
	return res
}

func chunkID(sourceRef, level string, groupIdx, subIdx int) string {
	h := sha1.Sum([]byte(sourceRef))
	return level + "_" + hex.EncodeToString(h[:4]) + "_" + itoa2(groupIdx) + "_" + itoa2(subIdx)
}

func itoa2(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}
	return digits
}
