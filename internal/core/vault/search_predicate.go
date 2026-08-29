package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	queryutil "go.lumeweb.com/queryutil/filter"
	"go.lumeweb.com/queryutil/filter/builder"
	"gorm.io/gorm"
)

// Predicate is one entry in a SearchRequest.Where list. The top-level Where
// list is ANDed; a field whose value is a LIST is OR/IN on that field, and a
// scalar value is equality (or prefix for Dir, inclusive lower bound for
// Since). A predicate carries exactly one field key — not wrapping another
// predicate, or a single field (Tag/Status/Source/Host/Agent/Dir/Since/Before)
// — enforced by ParseWhere. Unknown fields are rejected, so the schema is
// closed.
type Predicate struct {
	// Tag/Status/Source/Host/Agent/Dir are a scalar OR a list; a scalar is
	// normalized to a one-element slice during parsing. Length 1 = equality;
	// length > 1 = OR/IN on that field.
	Tag    []string `json:"tag,omitempty"`
	Status []string `json:"status,omitempty"`
	Source []string `json:"source,omitempty"`
	Host   []string `json:"host,omitempty"`
	Agent  []string `json:"agent,omitempty"`
	Dir    []string `json:"dir,omitempty"`
	// Since/Before are inclusive lower bound and exclusive upper bound on
	// files.created_at respectively; they are scalars (RFC3339 or YYYY-MM-DD).
	Since  string `json:"since,omitempty"`
	Before string `json:"before,omitempty"`
	// Not negates a single predicate.
	Not *Predicate `json:"not,omitempty"`
}

// SearchRequest is the structured input to Search. Query is the opaque name
// substring (never parsed as a filter language); Where is the ANDed predicate
// list; Limit caps the result set (default 500).
type SearchRequest struct {
	Query string      `json:"query,omitempty"`
	Where []Predicate `json:"where,omitempty"`
	Limit int         `json:"limit,omitempty"`
}

// predicateListFields are the field keys that accept a scalar or a list.
var predicateListFields = []string{"tag", "status", "source", "host", "agent", "dir"}
var predicateScalarFields = []string{"since", "before"}

// ParseWhere parses the `where` value into an ANDed predicate list. raw may be
// a JSON-encoded string (the CLI --where escape hatch) or a decoded []any of
// objects (the MCP surface). Each predicate object must carry exactly one
// field key (a scalar or a list), or a `not` wrapping a single predicate.
// Unknown fields, empty lists, and multi-field objects are errors.
func ParseWhere(raw any) ([]Predicate, error) {
	if raw == nil {
		return nil, nil
	}
	// CLI path: a JSON string.
	if s, ok := raw.(string); ok {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			return nil, nil
		}
		var decoded []any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
			return nil, fmt.Errorf("where: invalid JSON array: %w", err)
		}
		raw = decoded
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("where: must be an array of predicate objects, got %T", raw)
	}
	preds := make([]Predicate, 0, len(items))
	for _, it := range items {
		p, err := parseOnePredicate(it)
		if err != nil {
			return nil, err
		}
		preds = append(preds, p)
	}
	return preds, nil
}

// parseOnePredicate parses a single predicate object with validation.
func parseOnePredicate(raw any) (Predicate, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return Predicate{}, fmt.Errorf("where: each predicate must be an object, got %T", raw)
	}
	// Unknown fields are an error — the schema is closed.
	for k := range m {
		if k != "not" && !isPredicateField(k) {
			return Predicate{}, fmt.Errorf("where: unknown field %q", k)
		}
	}
	// `not` wrapper.
	if n, ok := m["not"]; ok {
		inner, err := parseOnePredicate(n)
		if err != nil {
			return Predicate{}, err
		}
		return Predicate{Not: &inner}, nil
	}
	// Count the field keys present (excluding not handled above).
	present := presentPredicateFields(m)
	if len(present) == 0 {
		return Predicate{}, errors.New("where: a predicate must have a field or not")
	}
	if len(present) > 1 {
		return Predicate{}, fmt.Errorf("where: a predicate must have exactly one field, got %v", present)
	}
	field := present[0]
	if isScalarPredicateField(field) {
		v, ok := m[field].(string)
		if !ok || v == "" {
			return Predicate{}, fmt.Errorf("where: %s must be a non-empty string", field)
		}
		if field == "since" {
			return Predicate{Since: v}, nil
		}
		return Predicate{Before: v}, nil
	}
	vals, err := predicateStringList(field, m[field])
	if err != nil {
		return Predicate{}, err
	}
	p := Predicate{}
	switch field {
	case "tag":
		p.Tag = vals
	case "status":
		// Status and source values are stored lowercase ("ok"/"pending"/"lost",
		// "mcp"/"cli"). Lower them here so a mixed-case where payload that
		// passes the schema matches stored rows (column matching is
		// case-sensitive).
		p.Status = lowerList(vals)
	case "source":
		p.Source = lowerList(vals)
	case "host":
		p.Host = vals
	case "agent":
		p.Agent = vals
	case "dir":
		p.Dir = vals
	}
	return p, nil
}

// lowerList lowercases every element of vals. Status/source are the only two
// predicate values that are normalized to a canonical lowercase form at write
// time, so they are the only fields lowered on parse.
func lowerList(vals []string) []string {
	return lo.Map(vals, func(v string, _ int) string { return strings.ToLower(v) })
}

// presentPredicateFields returns the field keys present in m (excluding not).
func presentPredicateFields(m map[string]any) []string {
	var out []string
	for _, f := range append(append([]string{}, predicateListFields...), predicateScalarFields...) {
		if _, ok := m[f]; ok {
			out = append(out, f)
		}
	}
	return out
}

func isPredicateField(k string) bool {
	for _, f := range predicateListFields {
		if k == f {
			return true
		}
	}
	for _, f := range predicateScalarFields {
		if k == f {
			return true
		}
	}
	return false
}

func isScalarPredicateField(k string) bool {
	for _, f := range predicateScalarFields {
		if k == f {
			return true
		}
	}
	return false
}

// predicateStringList normalizes a field value to a non-empty []string,
// accepting a scalar string (→ one element) or a list of strings.
func predicateStringList(field string, raw any) ([]string, error) {
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("where: %s must not be empty", field)
		}
		return []string{v}, nil
	case []any:
		if len(v) == 0 {
			return nil, fmt.Errorf("where: %s list must not be empty", field)
		}
		out := make([]string, 0, len(v))
		for _, it := range v {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("where: %s list entries must be strings, got %T", field, it)
			}
			if strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("where: %s list must not contain empty entries", field)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("where: %s must be a string or list of strings, got %T", field, raw)
	}
}

// parsePredicateTime parses a since/before value: RFC3339 or YYYY-MM-DD.
func parsePredicateTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("where: invalid time %q (want RFC3339 or YYYY-MM-DD)", s)
}

// tagPred groups the (normalized) tag names of one positive or negated tag
// predicate. not=true means "file must not have ANY of these tags".
type tagPred struct {
	names []string
	not   bool
}

// dirPred groups the directory prefixes of one positive or negated dir
// predicate. not=true means "file not under any of these prefixes".
type dirPred struct {
	paths []string
	not   bool
}

// applyWhere compiles the predicate list into the query with AND semantics. It
// returns a new query pointer (GORM clause chains are immutable) or an error
// for an invalid predicate. Tag predicates become file_tags join subqueries,
// dir predicates become path-prefix LIKE clauses, and the remaining column
// predicates (host/source/agent/status/created-at) are compiled to queryutil
// CrudFilters and applied through its GORM builder (which, since queryutil
// v0.3.19, treats a dotted field as a qualified column when its leading
// identifier is a known table, so "files.created_at" resolves correctly).
func (s *vaultService) applyWhere(q *gorm.DB, where []Predicate) (*gorm.DB, error) {
	var colQueries []queryutil.CrudFilter
	for _, p := range where {
		col, tag, dp, err := decomposePredicate(p)
		if err != nil {
			return nil, err
		}
		if tag != nil {
			sub, err := s.tagSubquery(tag.names, tag.not)
			if err != nil {
				return nil, err
			}
			q = sub(q)
		}
		if dp != nil {
			q = dirCondition(q, dp)
		}
		if col != nil {
			colQueries = append(colQueries, col)
		}
	}
	if len(colQueries) > 0 {
		b := builder.NewGORMBuilder(q, nil)
		var err error
		if q, err = b.Apply(q, colQueries); err != nil {
			return nil, fmt.Errorf("search: compile where: %w", err)
		}
	}
	return q, nil
}

// decomposePredicate splits one predicate into its compiled parts. Exactly one
// is non-nil (a predicate carries one field key; tag/dir/column cannot
// combine). col is a queryutil filter (host/source/agent/status/created_at
// including the not-wrapped forms); tag a file_tags join predicate; dir a path
// -prefix predicate.
func decomposePredicate(p Predicate) (queryutil.CrudFilter, *tagPred, *dirPred, error) {
	// A Not wrapper: dispatch on the inner predicate's field.
	if p.Not != nil {
		inner := *p.Not
		switch {
		case len(inner.Tag) > 0:
			return nil, &tagPred{names: inner.Tag, not: true}, nil, nil
		case len(inner.Dir) > 0:
			return nil, nil, &dirPred{paths: inner.Dir, not: true}, nil
		case inner.Since != "":
			t, err := parsePredicateTime(inner.Since)
			if err != nil {
				return nil, nil, nil, err
			}
			return queryutil.Not(queryutil.GreaterOrEqual("files.created_at", t)), nil, nil, nil
		case inner.Before != "":
			t, err := parsePredicateTime(inner.Before)
			if err != nil {
				return nil, nil, nil, err
			}
			return queryutil.Not(queryutil.LessThan("files.created_at", t)), nil, nil, nil
		default:
			return columnFilter(inner, true), nil, nil, nil
		}
	}
	switch {
	case len(p.Tag) > 0:
		return nil, &tagPred{names: p.Tag}, nil, nil
	case len(p.Dir) > 0:
		return nil, nil, &dirPred{paths: p.Dir}, nil
	case p.Since != "":
		t, err := parsePredicateTime(p.Since)
		if err != nil {
			return nil, nil, nil, err
		}
		return queryutil.GreaterOrEqual("files.created_at", t), nil, nil, nil
	case p.Before != "":
		t, err := parsePredicateTime(p.Before)
		if err != nil {
			return nil, nil, nil, err
		}
		return queryutil.LessThan("files.created_at", t), nil, nil, nil
	default:
		return columnFilter(p, false), nil, nil, nil
	}
}

// columnFilter builds a queryutil filter for the host/source/agent/status
// columns, handling scalar (equality) and list (IN) forms and optional
// negation. Column names are qualified ("files.host"): queryutil disambiguates
// a dotted field as a qualified column because "files" is a known table in the
// query's FROM clause (and "created_at" is ambiguous across the files/
// directories join otherwise, so created-at bounds use "files.created_at" too).
func columnFilter(p Predicate, negated bool) queryutil.CrudFilter {
	var column string
	var vals []string
	switch {
	case len(p.Host) > 0:
		column, vals = "files.host", p.Host
	case len(p.Source) > 0:
		column, vals = "files.source", p.Source
	case len(p.Agent) > 0:
		column, vals = "files.agent", p.Agent
	case len(p.Status) > 0:
		column, vals = "files.status", p.Status
	default:
		return nil
	}
	var f queryutil.CrudFilter
	if len(vals) == 1 {
		f = queryutil.Equal(column, vals[0])
	} else {
		anyVals := make([]any, len(vals))
		for i, v := range vals {
			anyVals[i] = v
		}
		f = queryutil.In(column, anyVals...)
	}
	if negated {
		f = queryutil.Not(f)
	}
	return f
}

// tagSubquery returns a query-modifier applying a tag predicate. Positive:
// files.id IN (files linked to any of the tag names). Negative: files.id NOT IN
// (files linked to any) implemented with a NOT-EXISTS to avoid the NOT-IN-NULL
// pitfall. Tag names are normalized (lowercased, deduped).
func (s *vaultService) tagSubquery(names []string, not bool) (func(*gorm.DB) *gorm.DB, error) {
	want := normalizeTags(names)
	if len(want) == 0 {
		return func(q *gorm.DB) *gorm.DB { return q }, nil
	}
	if not {
		return func(q *gorm.DB) *gorm.DB {
			return q.Where("NOT EXISTS (SELECT 1 FROM file_tags ft JOIN tags t ON t.id = ft.tag_id WHERE ft.file_id = files.id AND t.name IN ?)", want)
		}, nil
	}
	sub := s.db.Table("tags").
		Select("ft.file_id").
		Joins("JOIN file_tags ft ON ft.tag_id = tags.id").
		Where("tags.name IN ?", want).
		Group("ft.file_id").
		Having("COUNT(DISTINCT tags.name) >= 1")
	return func(q *gorm.DB) *gorm.DB {
		return q.Where("files.id IN (?)", sub)
	}, nil
}

// dirCondition applies a dir predicate as an OR of (exact path OR prefix LIKE)
// clauses over the directories join, optionally negated.
func dirCondition(q *gorm.DB, dp *dirPred) *gorm.DB {
	if len(dp.paths) == 0 {
		return q
	}
	var conds []string
	var params []any
	for _, d := range dp.paths {
		dir := normDirPrefix(d)
		conds = append(conds, "(directories.path = ? OR directories.path LIKE ? ESCAPE '\\')")
		params = append(params, dir, escapeLike(dir+"/")+"%")
	}
	expr := "(" + strings.Join(conds, " OR ") + ")"
	if dp.not {
		expr = "NOT " + expr
	}
	return q.Where(expr, params...)
}

// normDirPrefix normalizes a dir predicate value to the stored directory path,
// scheme-aware with a trailing slash trimmed and root "/" (matching how vault
// paths are stored and how List resolves them).
func normDirPrefix(d string) string {
	if IsVaultPath(d) {
		if vp, err := ParseVaultPath(d); err == nil {
			d = vp.Directory
			if vp.Name != "" && !vp.IsDir {
				d = JoinDirPath(vp.Directory, vp.Name)
			}
		}
	}
	d = strings.TrimSuffix(d, "/")
	if d == "" {
		d = "/"
	}
	return d
}
