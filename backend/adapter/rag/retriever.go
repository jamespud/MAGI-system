package rag

import (
	"context"
	"fmt"
	"log"
	"sort"
)

// MergedBlock is a retrieval result block at one of the three levels.
type MergedBlock struct {
	Level     int
	Content   string
	SourceRef string
	ChunkIDs  []string
}

// MergeOpts configures retrieval fusion and dynamic merge.
type MergeOpts struct {
	TopK    int
	RRFK    int
	Thr900  int
	Thr1800 int
	Orphan  string // "keep_300" | "pull_900"
}

// Retriever fuses vector + lexical recall and merges up the hierarchy.
type Retriever struct {
	vec  VectorIndex
	lex  LexicalIndex
	emb  Embedder
	repo *ChunkRepository
	opts MergeOpts
	log  *log.Logger
}

func NewRetriever(vec VectorIndex, lex LexicalIndex, emb Embedder, repo *ChunkRepository, opts MergeOpts) *Retriever {
	if opts.TopK == 0 {
		opts.TopK = 15
	}
	if opts.RRFK == 0 {
		opts.RRFK = 60
	}
	if opts.Thr900 == 0 {
		opts.Thr900 = 3
	}
	if opts.Thr1800 == 0 {
		opts.Thr1800 = 2
	}
	if opts.Orphan == "" {
		opts.Orphan = "keep_300"
	}
	return &Retriever{vec: vec, lex: lex, emb: emb, repo: repo, opts: opts, log: log.Default()}
}

// Retrieve runs hybrid recall + RRF + dynamic merge.
func (r *Retriever) Retrieve(ctx context.Context, query string, optsOverride MergeOpts) ([]MergedBlock, error) {
	opts := r.opts
	if optsOverride.TopK != 0 {
		opts = optsOverride
	}

	qvec, err := r.emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	var queryVec []float32
	if len(qvec) > 0 {
		queryVec = qvec[0]
	}

	vecHits, vecErr := r.vec.Search(ctx, queryVec, opts.TopK, nil)
	lexHits, lexErr := r.lex.Search(ctx, query, opts.TopK, nil)
	if vecErr != nil && lexErr != nil {
		r.logf("rag retrieve: BOTH indexes failed (query=%q): vector=%v lexical=%v", query, vecErr, lexErr)
		return nil, fmt.Errorf("rag retrieve: vector %v; lexical %v", vecErr, lexErr)
	}
	if vecErr != nil {
		r.logf("rag retrieve: vector index degraded, falling back to lexical only (query=%q): %v", query, vecErr)
	}
	if lexErr != nil {
		r.logf("rag retrieve: lexical index degraded, falling back to vector only (query=%q): %v", query, lexErr)
	}

	fused := rrf(vecHits, lexHits, opts.RRFK, opts.TopK)
	if len(fused) == 0 {
		return nil, nil
	}

	ids := make([]string, len(fused))
	for i, f := range fused {
		ids[i] = f.ChunkID
	}
	parents, err := r.repo.Get300Parents(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Group 300s by parent_900_id.
	groups := map[string][]string{}
	for _, id := range ids {
		p := parents[id]
		groups[p] = append(groups[p], id)
	}

	// Unanimous 900 merge.
	var pulled900 []string
	pulledBy900 := map[string]bool{}
	for p900, members := range groups {
		if p900 != "" && len(members) >= opts.Thr900 {
			pulled900 = append(pulled900, p900)
			for _, m := range members {
				pulledBy900[m] = true
			}
		}
	}

	var orphan300 []string
	for _, id := range ids {
		if !pulledBy900[id] {
			orphan300 = append(orphan300, id)
		}
	}

	blocks := []MergedBlock{}
	var nineHundreds []Chunk900
	if len(pulled900) > 0 {
		nineHundreds, err = r.repo.Get900Blocks(ctx, pulled900)
		if err != nil {
			return nil, err
		}
	}

	// Unanimous 1800 merge.
	groups1800 := map[string][]string{}
	for _, c9 := range nineHundreds {
		groups1800[c9.Parent1800ID] = append(groups1800[c9.Parent1800ID], c9.ID)
	}
	var pulled1800 []string
	pulled900By1800 := map[string]bool{}
	for p1800, members := range groups1800 {
		if p1800 != "" && len(members) >= opts.Thr1800 {
			pulled1800 = append(pulled1800, p1800)
			for _, m := range members {
				pulled900By1800[m] = true
			}
		}
	}

	if len(pulled1800) > 0 {
		eighteens, err := r.repo.Get1800Blocks(ctx, pulled1800)
		if err != nil {
			return nil, err
		}
		for _, c18 := range eighteens {
			blocks = append(blocks, MergedBlock{Level: 1800, Content: c18.Content, SourceRef: c18.SourceRef, ChunkIDs: groups1800[c18.ID]})
		}
	}
	for _, c9 := range nineHundreds {
		if !pulled900By1800[c9.ID] {
			blocks = append(blocks, MergedBlock{Level: 900, Content: c9.Content, SourceRef: c9.SourceRef, ChunkIDs: groups[c9.ID]})
		}
	}
	if opts.Orphan == "pull_900" {
		seen := map[string]bool{}
		for _, id := range orphan300 {
			p := parents[id]
			if p != "" && !seen[p] && !pulled900By1800[p] {
				seen[p] = true
				n9, err := r.repo.Get900Blocks(ctx, []string{p})
				if err == nil && len(n9) > 0 {
					blocks = append(blocks, MergedBlock{Level: 900, Content: n9[0].Content, SourceRef: n9[0].SourceRef, ChunkIDs: groups[p]})
				}
			}
		}
	} else {
		for _, id := range orphan300 {
			blocks = append(blocks, MergedBlock{Level: 300, Content: r.repo.get300Content(ctx, id), SourceRef: "", ChunkIDs: []string{id}})
		}
	}
	r.logf("rag retrieve: query=%q blocks=%d", query, len(blocks))
	return blocks, nil
}

// logf logs through r.log, falling back to the default logger when the
// Retriever was constructed directly (tests) without a logger.
func (r *Retriever) logf(format string, args ...any) {
	l := r.log
	if l == nil {
		l = log.Default()
	}
	l.Printf(format, args...)
}

// rrf fuses vector + lexical hits by Reciprocal Rank Fusion.
func rrf(vec []VectorHit, lex []TextHit, k, topK int) []VectorHit {
	scores := map[string]float64{}
	order := []string{}
	for i, h := range vec {
		if _, ok := scores[h.ChunkID]; !ok {
			order = append(order, h.ChunkID)
		}
		scores[h.ChunkID] += 1.0 / float64(k+i+1)
	}
	for i, h := range lex {
		if _, ok := scores[h.ChunkID]; !ok {
			order = append(order, h.ChunkID)
		}
		scores[h.ChunkID] += 1.0 / float64(k+i+1)
	}
	sort.Slice(order, func(i, j int) bool { return scores[order[i]] > scores[order[j]] })
	if len(order) > topK {
		order = order[:topK]
	}
	out := make([]VectorHit, len(order))
	for i, id := range order {
		out[i] = VectorHit{ChunkID: id, Score: float32(scores[id])}
	}
	return out
}
