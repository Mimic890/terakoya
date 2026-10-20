package store

import (
	"context"
	"time"
)

// LogSearch records a search: the query, normalized, and how many notes
// it found.
func (s *Store) LogSearch(ctx context.Context, query string, results int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO search_queries (query, results, at) VALUES (?, ?, ?)`,
		query, results, s.now().Unix())
	return err
}

// SearchCount is how often a query was searched for since some time, and
// how many notes it found the last time.
type SearchCount struct {
	Query   string `json:"query"`
	Count   int    `json:"count"`
	Results int    `json:"results"`
}

// TopSearches lists the n queries searched for most often since then.
func (s *Store) TopSearches(ctx context.Context, since time.Time, n int) ([]SearchCount, error) {
	return s.searches(ctx, since, n, false)
}

// MissingSearches lists the n queries searched for most often since then
// whose last search found nothing: the material users miss. A query that
// finds something now, once the material was added, drops out.
func (s *Store) MissingSearches(ctx context.Context, since time.Time, n int) ([]SearchCount, error) {
	return s.searches(ctx, since, n, true)
}

func (s *Store) searches(ctx context.Context, since time.Time, n int, missing bool) ([]SearchCount, error) {
	// With MAX(at), SQLite takes results from the latest row of a group.
	rows, err := s.db.QueryContext(ctx, `
		SELECT query, COUNT(*) AS n, results, MAX(at) AS last FROM search_queries WHERE at >= ?
		GROUP BY query HAVING NOT ? OR results = 0
		ORDER BY n DESC, last DESC, query LIMIT ?`, since.Unix(), missing, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchCount
	for rows.Next() {
		var c SearchCount
		var last int64
		if err := rows.Scan(&c.Query, &c.Count, &c.Results, &last); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
