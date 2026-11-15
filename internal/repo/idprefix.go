package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// MinIDPrefixLen is the shortest ID prefix accepted in place of a full ULID.
// Table output and add confirmations print the first 8 characters.
const MinIDPrefixLen = 8

const ulidLen = 26

// crockford is the ULID alphabet (Crockford base32, upper case).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// resolveIDPrefix expands a short ID prefix to the full ULID of the single
// row in table whose id starts with it. A full ULID, a ref outside the ULID
// alphabet, a ref shorter than MinIDPrefixLen, and a prefix with no match
// come back unchanged, so the caller's own lookup reports not found as
// before. A prefix that matches more than one row is an error.
// table and kind come from code, never from user input.
func resolveIDPrefix(ctx context.Context, db *sql.DB, table, kind, ref string) (string, error) {
	prefix := strings.ToUpper(strings.TrimSpace(ref))
	if len(prefix) < MinIDPrefixLen || len(prefix) >= ulidLen {
		return ref, nil
	}
	for _, c := range prefix {
		if !strings.ContainsRune(crockford, c) {
			return ref, nil
		}
	}
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM `+table+` WHERE id LIKE ? LIMIT 2`, prefix+"%")
	if err != nil {
		return "", fmt.Errorf("resolve %s id %q: %w", kind, ref, err)
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("resolve %s id %q: %w", kind, ref, err)
		}
		matches = append(matches, id)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("resolve %s id %q: %w", kind, ref, err)
	}
	switch len(matches) {
	case 0:
		return ref, nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%s id %q matches more than one %s: use the full ID from --json", kind, ref, kind)
	}
}

// ResolveID expands a short doc ID prefix to the full ID.
func (r *DocRepo) ResolveID(ctx context.Context, ref string) (string, error) {
	return resolveIDPrefix(ctx, r.db, "project_docs", "doc", ref)
}

// ResolveID expands a short memory ID prefix to the full ID.
func (r *MemoryRepo) ResolveID(ctx context.Context, ref string) (string, error) {
	return resolveIDPrefix(ctx, r.db, "project_memory", "memory", ref)
}

// ResolveID expands a short plan ID prefix to the full ID.
func (r *PlanRepo) ResolveID(ctx context.Context, ref string) (string, error) {
	return resolveIDPrefix(ctx, r.db, "plans", "plan", ref)
}

// ResolveID expands a short attachment ID prefix to the full ID.
func (r *AttachmentRepo) ResolveID(ctx context.Context, ref string) (string, error) {
	return resolveIDPrefix(ctx, r.db, "attachments", "attachment", ref)
}

// ResolveID expands a short comment ID prefix to the full ID.
func (r *CommentRepo) ResolveID(ctx context.Context, ref string) (string, error) {
	return resolveIDPrefix(ctx, r.db, "comments", "comment", ref)
}
