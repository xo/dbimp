package cosmos

import (
	"context"
	"encoding/json/v2"
	"net"
	"strings"
	"time"
)

// The catalog statements of D201: the paths of the indexing policy, the unique
// keys, the routines, the ranges of the partition keys and the session. They
// follow the form of the statements in catalog.go.

// indexPathColumns are the columns of $index_paths, one row for each path of
// one entry of the indexing policy.
var indexPathColumns = []column{
	{"database", "string", "the WHERE"},
	{"container", "string", "the WHERE"},
	{"path", "string", "the path of the entry, such as /a/? or /*"},
	{"kind", "string", "included, excluded, composite, spatial, vector or full_text, from the list of the policy that holds the path"},
	{"path_order", "string", "asc or desc for a path of a composite index, from order, and nil for the other kinds"},
	{"position", "int64", "the position of the path inside its composite index, from 0, and nil for the other kinds"},
	{"composite_group", "int64", "the number of the composite index in compositeIndexes, from 0, and nil for the other kinds"},
	{"spatial_types", "[]any of string", "spatialIndexes, the types of the path, and nil for the other kinds"},
	{"vector_index_type", "string", "vectorIndexes, the type of the index, such as flat, quantizedFlat or diskANN, and nil for the other kinds"},
	{"dimensions", "int64", "vectorEmbeddingPolicy.vectorEmbeddings, the dimensions of the vector with the same path, and nil when the policy has none"},
	{"full_text_path", "string", "fullTextIndexes, the path of the index, and nil for the other kinds"},
}

// indexRow returns a row of $index_paths. The columns after the kind are nil.
func indexRow(db, coll string, path, kind any) []any {
	row := make([]any, len(indexPathColumns))
	row[0], row[1], row[2], row[3] = db, coll, path, kind
	return row
}

// fetchIndexPaths returns the paths of the indexing policy of a container.
func fetchIndexPaths(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db, coll := w["database"], w["container"]
	d, err := c.resource(ctx, collURL(db, coll))
	if err != nil {
		return nil, err
	}
	pol := at(d, "indexingPolicy")
	rows := [][]any{}
	for _, list := range []struct{ name, kind string }{{"includedPaths", "included"}, {"excludedPaths", "excluded"}} {
		l, _ := at(pol, list.name).([]any)
		for _, e := range l {
			rows = append(rows, indexRow(db, coll, str(at(e, "path")), list.kind))
		}
	}
	groups, _ := at(pol, "compositeIndexes").([]any)
	for g, group := range groups {
		paths, _ := group.([]any)
		for i, p := range paths {
			row := indexRow(db, coll, str(at(p, "path")), "composite")
			switch at(p, "order") {
			case "ascending":
				row[4] = "asc"
			case "descending":
				row[4] = "desc"
			}
			row[5], row[6] = int64(i), int64(g)
			rows = append(rows, row)
		}
	}
	spatial, _ := at(pol, "spatialIndexes").([]any)
	for _, e := range spatial {
		row := indexRow(db, coll, str(at(e, "path")), "spatial")
		row[7] = array(at(e, "types"))
		rows = append(rows, row)
	}
	dims := map[string]any{}
	embeddings, _ := at(d, "vectorEmbeddingPolicy", "vectorEmbeddings").([]any)
	for _, e := range embeddings {
		if p, ok := at(e, "path").(string); ok {
			dims[p] = whole(at(e, "dimensions"))
		}
	}
	vectors, _ := at(pol, "vectorIndexes").([]any)
	for _, e := range vectors {
		row := indexRow(db, coll, str(at(e, "path")), "vector")
		row[8] = str(at(e, "type"))
		if p, ok := at(e, "path").(string); ok {
			row[9] = dims[p]
		}
		rows = append(rows, row)
	}
	texts, _ := at(pol, "fullTextIndexes").([]any)
	for _, e := range texts {
		row := indexRow(db, coll, str(at(e, "path")), "full_text")
		row[10] = str(at(e, "path"))
		rows = append(rows, row)
	}
	return rows, nil
}

// fetchUniqueKeys returns the unique keys of a container, one row for each key.
func fetchUniqueKeys(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db, coll := w["database"], w["container"]
	d, err := c.resource(ctx, collURL(db, coll))
	if err != nil {
		return nil, err
	}
	keys, _ := at(d, "uniqueKeyPolicy", "uniqueKeys").([]any)
	rows := make([][]any, len(keys))
	for i := range keys {
		rows[i] = []any{db, coll, int64(i)}
	}
	return rows, nil
}

// fetchUniqueKeyPaths returns the paths of the unique keys of a container, one
// row for each path.
func fetchUniqueKeyPaths(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db, coll := w["database"], w["container"]
	d, err := c.resource(ctx, collURL(db, coll))
	if err != nil {
		return nil, err
	}
	keys, _ := at(d, "uniqueKeyPolicy", "uniqueKeys").([]any)
	rows := [][]any{}
	for i, k := range keys {
		paths, _ := at(k, "paths").([]any)
		for j, p := range paths {
			rows = append(rows, []any{db, coll, int64(i), int64(j), str(p)})
		}
	}
	return rows, nil
}

// stamp returns the _ts of a resource, which is in seconds since the epoch, as
// a time in UTC, and nil when v is not a whole number.
func stamp(v any) any {
	n, ok := whole(v).(int64)
	if !ok {
		return nil
	}
	return time.Unix(n, 0).UTC()
}

// fetchRoutines returns the stored procedures and then the user defined
// functions of a container.
func fetchRoutines(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db, coll := w["database"], w["container"]
	rows := [][]any{}
	for _, f := range []struct{ path, key, kind string }{
		{"sprocs", "StoredProcedures", "stored_procedure"},
		{"udfs", "UserDefinedFunctions", "function"},
	} {
		list, err := c.list(ctx, collURL(db, coll)+"/"+f.path, f.key)
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			rows = append(rows, []any{db, coll, f.kind, str(d["id"]), str(d["body"]), str(d["_rid"]), str(d["_etag"]), stamp(d["_ts"])})
		}
	}
	return rows, nil
}

// fetchPartitionKeyRanges returns the ranges of the partition keys of a
// container.
func fetchPartitionKeyRanges(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db, coll := w["database"], w["container"]
	list, err := c.list(ctx, collURL(db, coll)+"/pkranges", "PartitionKeyRanges")
	if err != nil {
		return nil, err
	}
	rows := make([][]any, len(list))
	for i, d := range list {
		rows[i] = []any{db, coll, str(d["id"]), str(d["minInclusive"]), str(d["maxExclusive"]), str(d["status"]), array(d["parents"])}
	}
	return rows, nil
}

// sessionRow returns the row of $session, which holds what the connector and the
// options of the statement say, and sends no request.
func sessionRow(c *Connector, o options) [][]any {
	cfg := c.cfg
	port := cfg.Port
	switch {
	case port != 0:
	case cfg.TLS:
		port = 443
	default:
		port = 80
	}
	account := cfg.Host
	if net.ParseIP(cfg.Host) == nil {
		account, _, _ = strings.Cut(cfg.Host, ".")
	}
	var database, container, key any
	if o.database != "" {
		database = o.database
	}
	if o.container != "" {
		container = o.container
	}
	if o.hasKey {
		var l []any
		if json.Unmarshal([]byte(o.key), &l) == nil && len(l) == 1 {
			key = l[0]
		}
	}
	return [][]any{{cfg.Host, int64(port), account, database, container, cfg.TLS, key}}
}
