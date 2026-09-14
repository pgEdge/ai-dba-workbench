/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package embedding

import "fmt"

// MaxDimensions is the fixed width of the halfvec columns used to store
// embeddings. The vector tables (chat_memories.embedding and
// anomaly_embeddings.embedding) are declared as halfvec(4000); every
// embedding is right-padded with zeros to exactly this width before it
// is stored or used as a query vector. The halfvec HNSW index supports
// up to 4000 dimensions, which covers every provider currently in use
// (the largest, Gemini, emits 3072). Trailing zeros do not change the
// cosine distance between two equally padded vectors, so similarity
// search results are unaffected.
const MaxDimensions = 4000

// PadTo returns a new slice containing vec right-padded with zeros to
// exactly dim elements. It is the single point that enforces the
// MaxDimensions ceiling for both stored and query vectors.
//
// A nil or empty input returns nil: callers treat a missing embedding
// as "no vector available" and must not fabricate an all-zero vector,
// which would otherwise pollute similarity search.
//
// PadTo returns an error when len(vec) > dim. Models that emit more
// than dim dimensions are unsupported; the embedding must be rejected
// rather than silently truncated, because truncation would discard
// signal and corrupt distances.
func PadTo(vec []float32, dim int) ([]float32, error) {
	if len(vec) == 0 {
		return nil, nil
	}
	if len(vec) > dim {
		return nil, fmt.Errorf(
			"embedding has %d dimensions, exceeds maximum supported %d",
			len(vec), dim)
	}
	// A new slice of len dim is already zero-filled; copying the input
	// over the leading elements right-pads with zeros. Returning a copy
	// (never the caller's slice) keeps the result independent of the
	// input even when len(vec) == dim.
	out := make([]float32, dim)
	copy(out, vec)
	return out, nil
}
