package cosmos

import (
	"context"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/xo/dbimp"
)

// The catalog statements (D190 item 17) answer what a model of the catalog needs
// and the SQL of Cosmos DB cannot say: the databases, the containers with their
// policies, the stored procedures, the triggers, the functions, the throughput,
// the users, the permissions and the account. A statement has one form:
//
//	SELECT * FROM "$containers" WHERE database = 'db'
//
// The name after FROM is one of the reserved names below, in double quotes or
// bare, and the SELECT list is *. The WHERE holds conditions joined by AND, and
// each condition is a key, =, and a value. The key is database, container or
// user, and the value is a string in single quotes or a named argument such as
// @db. Each key appears once. The path of the DSN and the options WithDatabase
// and WithContainer give the database and the container that a WHERE leaves out.
// Any other statement goes to the container as a query, as before. The driver
// reads the REST API for each statement, and returns one flat set of rows. A
// value that the server leaves out is nil.

// column is one column of a catalog statement.
type column struct {
	// Name is the name of the column.
	Name string
	// Type is the Go type of its value, in the words of the type table. A
	// value that the server leaves out is nil, whatever the type is.
	Type string
	// Note says where the value comes from.
	Note string
}

// catalog is one reserved name.
type catalog struct {
	// Name is the reserved name, with its $.
	Name string
	// Needs are the keys that the WHERE or the DSN must give, and Allows the
	// keys that it can give besides.
	Needs, Allows []string
	// Columns are the columns of the rows.
	Columns []column
	// fetch reads the rows, with the values of the keys.
	fetch func(ctx context.Context, c *Connector, w map[string]string) ([][]any, error)
}

// links is the pair of columns that every resource has.
var links = []column{
	{"rid", "string", "_rid"},
	{"link", "string", "_self"},
}

// scriptColumns are the columns of the stored procedures, the triggers and the
// functions, and extra are the columns that follow the body.
func scriptColumns(extra ...column) []column {
	cols := []column{
		{"database", "string", "the WHERE"},
		{"container", "string", "the WHERE"},
		{"id", "string", "id"},
	}
	cols = append(cols, links...)
	cols = append(cols, column{"body", "string", "body, which is free-form code"})
	return append(cols, extra...)
}

// catalogs returns the catalog statements, in the order of this list.
func catalogs() []catalog {
	return []catalog{
		{
			Name: "$account",
			Columns: []column{
				{"id", "string", "id"},
				{"rid", "string", "_rid"},
				{"writable_regions", "[]any of string", "writableLocations, the name of each"},
				{"writable_endpoints", "[]any of string", "writableLocations, the endpoint of each"},
				{"readable_regions", "[]any of string", "readableLocations, the name of each"},
				{"readable_endpoints", "[]any of string", "readableLocations, the endpoint of each"},
				{"default_consistency", "string", "userConsistencyPolicy.defaultConsistencyLevel"},
				{"multiple_write_locations", "bool", "enableMultipleWriteLocations"},
				{"continuous_backup", "bool", "continuousBackupEnabled"},
				{"query_engine_configuration", "string", "queryEngineConfiguration, which is JSON text"},
			},
			fetch: fetchAccount,
		},
		{
			Name:    "$databases",
			Columns: slices.Concat([]column{{"id", "string", "id"}}, links),
			fetch:   fetchDatabases,
		},
		{
			Name:    "$containers",
			Needs:   []string{"database"},
			Allows:  []string{"container"},
			Columns: containerColumns,
			fetch:   fetchContainers,
		},
		{
			Name:    "$stored_procedures",
			Needs:   []string{"database", "container"},
			Columns: scriptColumns(),
			fetch:   fetchScripts("sprocs", "StoredProcedures"),
		},
		{
			Name:  "$triggers",
			Needs: []string{"database", "container"},
			Columns: scriptColumns(
				column{"trigger_type", "string", "triggerType, Pre or Post"},
				column{"trigger_operation", "string", "triggerOperation, such as All, Create or Replace"}),
			fetch: fetchScripts("triggers", "Triggers"),
		},
		{
			Name:    "$functions",
			Needs:   []string{"database", "container"},
			Columns: scriptColumns(),
			fetch:   fetchScripts("udfs", "UserDefinedFunctions"),
		},
		{
			Name:   "$offers",
			Needs:  []string{"database"},
			Allows: []string{"container"},
			Columns: []column{
				{"scope", "string", "database or container"},
				{"database", "string", "the WHERE"},
				{"container", "string", "the container of a container offer, and nil for a database offer"},
				{"id", "string", "id"},
				{"rid", "string", "_rid"},
				{"resource_link", "string", "resource"},
				{"version", "string", "offerVersion"},
				{"offer_type", "string", "offerType"},
				{"throughput", "int64", "content.offerThroughput, the manual request units a second, and nil for autoscale"},
				{"autoscale_max_throughput", "int64", "content.offerAutopilotSettings.maxThroughput, and nil for manual"},
				{"autoscale_increment_percent", "int64", "content.offerAutopilotSettings.autoUpgradePolicy.throughputPolicy.incrementPercent"},
			},
			fetch: fetchOffers,
		},
		{
			Name:  "$users",
			Needs: []string{"database"},
			Columns: slices.Concat([]column{
				{"database", "string", "the WHERE"},
				{"id", "string", "id"},
			}, links),
			fetch: fetchUsers,
		},
		{
			Name:   "$permissions",
			Needs:  []string{"database"},
			Allows: []string{"user"},
			Columns: slices.Concat([]column{
				{"database", "string", "the WHERE"},
				{"user", "string", "the user that holds the permission"},
				{"id", "string", "id"},
			}, links, []column{
				{"permission_mode", "string", "permissionMode, Read or All"},
				{"resource_link", "string", "resource"},
			}),
			fetch: fetchPermissions,
		},
	}
}

// containerColumns are the columns of $containers. A policy of a fixed shape is
// in flat columns. A list of indexes, and a policy that has no fixed shape, are
// decoded JSON values, and the whole indexing policy is also JSON text.
var containerColumns = []column{
	{"database", "string", "the WHERE"},
	{"id", "string", "id"},
	{"rid", "string", "_rid"},
	{"link", "string", "_self"},
	{"partition_key_paths", "[]any of string", "partitionKey.paths"},
	{"partition_key_kind", "string", "partitionKey.kind, such as Hash"},
	{"partition_key_version", "int64", "partitionKey.version"},
	{"indexing_mode", "string", "indexingPolicy.indexingMode"},
	{"indexing_automatic", "bool", "indexingPolicy.automatic"},
	{"indexing_included_paths", "[]any of string", "indexingPolicy.includedPaths, the path of each"},
	{"indexing_excluded_paths", "[]any of string", "indexingPolicy.excludedPaths, the path of each"},
	{"composite_indexes", "[]any of []any of map[string]any", "indexingPolicy.compositeIndexes, each with path and order"},
	{"spatial_indexes", "[]any of map[string]any", "indexingPolicy.spatialIndexes"},
	{"vector_indexes", "[]any of map[string]any", "indexingPolicy.vectorIndexes"},
	{"indexing_policy", "string", "indexingPolicy, as JSON text with sorted keys"},
	{"unique_keys", "[]any of []any of string", "uniqueKeyPolicy.uniqueKeys, the paths of each key"},
	{"default_ttl", "int64", "defaultTtl, in seconds, and -1 for no expiry by default"},
	{"analytical_ttl", "int64", "analyticalStorageTtl"},
	{"conflict_resolution_mode", "string", "conflictResolutionPolicy.mode"},
	{"conflict_resolution_path", "string", "conflictResolutionPolicy.conflictResolutionPath"},
	{"conflict_resolution_procedure", "string", "conflictResolutionPolicy.conflictResolutionProcedure"},
	{"change_feed_retention", "int64", "changeFeedPolicy.retentionDuration, in minutes"},
	{"computed_properties", "[]any of map[string]any", "computedProperties, each with name and query"},
	{"vector_embedding_policy", "map[string]any", "vectorEmbeddingPolicy"},
	{"full_text_policy", "map[string]any", "fullTextPolicy"},
	{"geospatial_type", "string", "geospatialConfig.type"},
}

// The keys that a WHERE can name.
var whereKeys = []string{"database", "container", "user"}

// catalogStatement is a statement that the driver answers itself.
type catalogStatement struct {
	catalog catalog
	where   map[string]string
}

// parseCatalog reports whether the statement is a catalog statement, which is a
// SELECT * FROM with a reserved name. If it is not, the statement goes to the
// container. If it is, and the rest of it does not follow the grammar, the error
// says what is wrong. args are the arguments that are not options, which the
// values of the WHERE can name.
func parseCatalog(statement string, args []driver.NamedValue) (*catalogStatement, bool, error) {
	toks, ok := tokenize(stripSemicolon(statement))
	if !ok || len(toks) < 4 || !toks[0].is("select") {
		return nil, false, nil
	}
	from := slices.IndexFunc(toks, func(t token) bool { return t.is("from") })
	if from < 0 || from+1 >= len(toks) {
		return nil, false, nil
	}
	name := toks[from+1]
	if name.kind != tokWord && name.kind != tokQuoted {
		return nil, false, nil
	}
	var found *catalog
	all := catalogs()
	for i := range all {
		if all[i].Name == name.text {
			found = &all[i]
		}
	}
	if found == nil {
		return nil, false, nil
	}
	if from != 2 || toks[1].kind != tokStar {
		return nil, true, fmt.Errorf("reading the catalog statement: the list after SELECT must be a star for %s: %w", found.Name, dbimp.ErrInvalidValue)
	}
	w, err := parseWhere(toks[from+2:], args)
	if err != nil {
		return nil, true, fmt.Errorf("reading the catalog statement %s: %w", found.Name, err)
	}
	for k := range w {
		if !slices.Contains(found.Needs, k) && !slices.Contains(found.Allows, k) {
			return nil, true, fmt.Errorf("reading the catalog statement %s: the key %s is not one of %s: %w", found.Name, k, strings.Join(slices.Concat(found.Needs, found.Allows), ", "), dbimp.ErrInvalidValue)
		}
	}
	return &catalogStatement{catalog: *found, where: w}, true, nil
}

// parseWhere reads an optional WHERE of conditions joined by AND, and binds each
// named value to its argument. It refuses an argument that no condition uses.
func parseWhere(toks []token, args []driver.NamedValue) (map[string]string, error) {
	w := map[string]string{}
	used := map[string]bool{}
	if len(toks) > 0 {
		if !toks[0].is("where") {
			return nil, fmt.Errorf("%q follows the name, and only WHERE can: %w", toks[0].text, dbimp.ErrInvalidValue)
		}
		toks = toks[1:]
		for {
			if len(toks) < 3 || toks[0].kind != tokWord || toks[1].kind != tokEquals {
				return nil, fmt.Errorf("a condition is a key, =, and a value, such as database = 'db': %w", dbimp.ErrInvalidValue)
			}
			key := strings.ToLower(toks[0].text)
			if !slices.Contains(whereKeys, key) {
				return nil, fmt.Errorf("the key %q is not one of %s: %w", toks[0].text, strings.Join(whereKeys, ", "), dbimp.ErrInvalidValue)
			}
			if _, dup := w[key]; dup {
				return nil, fmt.Errorf("the key %s appears twice: %w", key, dbimp.ErrInvalidValue)
			}
			switch v := toks[2]; v.kind {
			case tokString:
				w[key] = v.text
			case tokParam:
				i := slices.IndexFunc(args, func(a driver.NamedValue) bool { return strings.TrimPrefix(a.Name, "@") == v.text })
				if i < 0 {
					return nil, fmt.Errorf("the argument @%s is not given: %w", v.text, dbimp.ErrArguments)
				}
				s, ok := args[i].Value.(string)
				if !ok {
					return nil, fmt.Errorf("the argument @%s is a %T, and a name is a string: %w", v.text, args[i].Value, dbimp.ErrArguments)
				}
				w[key], used[v.text] = s, true
			default:
				return nil, fmt.Errorf("the value of %s is a string in single quotes or a named argument: %w", key, dbimp.ErrInvalidValue)
			}
			toks = toks[3:]
			if len(toks) == 0 {
				break
			}
			if !toks[0].is("and") {
				return nil, fmt.Errorf("%q follows a condition, and only AND can: %w", toks[0].text, dbimp.ErrInvalidValue)
			}
			toks = toks[1:]
		}
	}
	for _, a := range args {
		if !used[strings.TrimPrefix(a.Name, "@")] {
			return nil, fmt.Errorf("the argument %d is not used by the WHERE: %w", a.Ordinal, dbimp.ErrArguments)
		}
	}
	return w, nil
}

// The kinds of token.
const (
	tokWord = iota
	tokQuoted
	tokString
	tokParam
	tokStar
	tokEquals
)

// token is one token of a catalog statement.
type token struct {
	kind int
	text string
}

// is reports whether the token is the word w, with no regard to case.
func (t token) is(w string) bool {
	return t.kind == tokWord && strings.EqualFold(t.text, w)
}

// tokenize splits a statement into words, quoted names, strings, parameters, *
// and =. It reports false for a character that no catalog statement has, so
// that the statement goes to the container. A -- comment ends at the end of its
// line.
func tokenize(s string) ([]token, bool) {
	var toks []token
	for i := 0; i < len(s); {
		ch := s[i]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			i++
		case ch == '-' && strings.HasPrefix(s[i:], "--"):
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				return toks, true
			}
			i += end
		case ch == '*':
			toks, i = append(toks, token{kind: tokStar, text: "*"}), i+1
		case ch == '=':
			toks, i = append(toks, token{kind: tokEquals, text: "="}), i+1
		case ch == '\'' || ch == '"':
			text, n, ok := quoted(s[i:])
			if !ok {
				return nil, false
			}
			kind := tokString
			if ch == '"' {
				kind = tokQuoted
			}
			toks, i = append(toks, token{kind: kind, text: text}), i+n
		case ch == '@' || isWordChar(ch):
			j := i + 1
			for j < len(s) && isWordChar(s[j]) {
				j++
			}
			kind, text := tokWord, s[i:j]
			if ch == '@' {
				kind, text = tokParam, s[i+1:j]
			}
			toks, i = append(toks, token{kind: kind, text: text}), j
		default:
			return nil, false
		}
	}
	return toks, true
}

// isWordChar reports whether ch can be in a word: a letter, a digit, an
// underscore or a $.
func isWordChar(ch byte) bool {
	return ch == '_' || ch == '$' || '0' <= ch && ch <= '9' || 'a' <= ch && ch <= 'z' || 'A' <= ch && ch <= 'Z'
}

// quoted reads the string or the name that starts s, which has the quote as its
// first byte. A quote is written twice inside, and a backslash escapes the next
// byte. It returns the text, and the number of bytes that it read.
func quoted(s string) (string, int, bool) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			i++
			b.WriteByte(s[i])
		case s[i] == q && i+1 < len(s) && s[i+1] == q:
			b.WriteByte(q)
			i++
		case s[i] == q:
			return b.String(), i + 1, true
		default:
			b.WriteByte(s[i])
		}
	}
	return "", 0, false
}

// run reads the catalog and returns the rows of the statement. o holds the
// database and the container of the DSN and of the options, for a key that the
// WHERE leaves out.
func (cs *catalogStatement) run(ctx context.Context, c *Connector, o options) (driver.Rows, error) {
	// The database and the container of the DSN and of the options fill a key
	// that the statement needs. A key that it only allows narrows the statement,
	// so it comes from the WHERE alone.
	w := map[string]string{}
	for k, v := range map[string]string{"database": o.database, "container": o.container} {
		if slices.Contains(cs.catalog.Needs, k) && v != "" {
			w[k] = v
		}
	}
	maps.Copy(w, cs.where)
	for _, k := range cs.catalog.Needs {
		if w[k] == "" {
			return nil, fmt.Errorf("reading the catalog statement %s: it needs WHERE %s = '...', or %s in the DSN or in an option: %w", cs.catalog.Name, k, k, dbimp.ErrInvalidValue)
		}
	}
	for _, k := range []string{"database", "container"} {
		if w[k] == "" {
			continue
		}
		if err := checkName(w[k]); err != nil {
			return nil, fmt.Errorf("reading the catalog statement %s: the %s: %w", cs.catalog.Name, k, err)
		}
	}
	rows, err := cs.catalog.fetch(ctx, c, w)
	if err != nil {
		return nil, err
	}
	cols := make([]string, len(cs.catalog.Columns))
	for i, col := range cs.catalog.Columns {
		cols[i] = col.Name
	}
	return &tableRows{cols: cols, rows: rows}, nil
}

// tableRows is a flat set of rows that the driver holds, which is a result of a
// catalog statement.
type tableRows struct {
	cols []string
	rows [][]any
	next int
	cur  []any
}

// ensure the interfaces.
var (
	_ driver.RowsColumnScanner      = (*tableRows)(nil)
	_ driver.RowsColumnTypeScanType = (*tableRows)(nil)
	_ driver.RowsColumnTypeNullable = (*tableRows)(nil)
)

// Columns satisfies driver.Rows.
func (r *tableRows) Columns() []string { return r.cols }

// Close satisfies driver.Rows.
func (r *tableRows) Close() error { return nil }

// ColumnTypeScanType satisfies driver.RowsColumnTypeScanType. The type of a
// value is in the table of docs/COSMOS.md, and a value that the server leaves out is
// nil, so the scan type is any, as it is for a query.
func (r *tableRows) ColumnTypeScanType(int) reflect.Type { return reflect.TypeFor[any]() }

// ColumnTypeNullable satisfies driver.RowsColumnTypeNullable. The server can
// leave out any value.
func (r *tableRows) ColumnTypeNullable(int) (bool, bool) { return true, true }

// NextRow satisfies driver.RowsColumnScanner.
func (r *tableRows) NextRow() error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	r.cur = r.rows[r.next]
	r.next++
	return nil
}

// Next satisfies driver.Rows.
func (r *tableRows) Next(dest []driver.Value) error {
	if err := r.NextRow(); err != nil {
		return err
	}
	for i, v := range r.cur {
		dest[i] = v
	}
	return nil
}

// ScanColumn satisfies driver.RowsColumnScanner.
func (r *tableRows) ScanColumn(scanCtx driver.ScanContext, i int, dest any) error {
	return dbimp.Assign(scanCtx, dest, r.cur[i])
}

// get reads the path of a resource, such as /dbs, with the header of a
// continuation, and returns the response.
func (c *Connector) get(ctx context.Context, path, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("making the request: %w", err)
	}
	if token != "" {
		req.Header.Set("X-Ms-Continuation", token)
	}
	return c.do(req)
}

// resource reads one resource, such as a database or the account, as the map of
// its members.
func (c *Connector) resource(ctx context.Context, path string) (map[string]any, error) {
	res, err := c.get(ctx, path, "")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var v jsontext.Value
	if err := json.UnmarshalRead(res.Body, &v); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return object(v, path)
}

// object decodes v as a map.
func object(v jsontext.Value, path string) (map[string]any, error) {
	m, err := dbimp.Any(v)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	obj, ok := m.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reading %s: a resource is not an object: %w", path, dbimp.ErrInvalidValue)
	}
	return obj, nil
}

// list reads a feed, such as /dbs, which holds its resources in the array key,
// and follows the continuation to the last page. The answer of a feed is small,
// so the driver holds a page of resources at a time.
func (c *Connector) list(ctx context.Context, path, key string) ([]map[string]any, error) {
	var out []map[string]any
	token := ""
	for {
		res, err := c.get(ctx, path, token)
		if err != nil {
			return nil, err
		}
		var page map[string]jsontext.Value
		err = json.UnmarshalRead(res.Body, &page)
		next := res.Header.Get("X-Ms-Continuation")
		res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var items []jsontext.Value
		if v, ok := page[key]; ok && !dbimp.IsNull(v) {
			if err := json.Unmarshal(v, &items); err != nil {
				return nil, fmt.Errorf("reading %s in %s: %w", key, path, err)
			}
		}
		for _, v := range items {
			obj, err := object(v, path)
			if err != nil {
				return nil, err
			}
			out = append(out, obj)
		}
		if next == "" {
			return out, nil
		}
		token = next
	}
}

// dbURL returns the path of a database.
func dbURL(db string) string { return "/dbs/" + url.PathEscape(db) }

// collURL returns the path of a container.
func collURL(db, coll string) string { return dbURL(db) + "/colls/" + url.PathEscape(coll) }

// at returns the value at the path of names in nested maps, and nil when a
// member is missing.
func at(v any, names ...string) any {
	for _, n := range names {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[n]
	}
	return v
}

// str returns v when it is a string, and else nil.
func str(v any) any {
	if s, ok := v.(string); ok {
		return s
	}
	return nil
}

// whole returns v as an int64 when it is a whole number, and else nil.
func whole(v any) any {
	switch v := v.(type) {
	case int64:
		return v
	case float64:
		if v == float64(int64(v)) {
			return int64(v)
		}
	}
	return nil
}

// flag returns v when it is a bool, and else nil.
func flag(v any) any {
	if b, ok := v.(bool); ok {
		return b
	}
	return nil
}

// array returns v when it is a list, and else nil.
func array(v any) any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// mapping returns v when it is a map, and else nil.
func mapping(v any) any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// members returns, for a list of maps, the list of the value of one member of
// each, such as the path of each included path. It returns nil when v is not a
// list.
func members(v any, name string) any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(l))
	for _, e := range l {
		out = append(out, at(e, name))
	}
	return out
}

// uniqueKeys returns the paths of each unique key, as a list of lists.
func uniqueKeys(v any) any {
	l, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(l))
	for _, k := range l {
		p, _ := at(k, "paths").([]any)
		if p == nil {
			p = []any{}
		}
		out = append(out, p)
	}
	return out
}

// jsonText returns the JSON text of v, with sorted keys, and nil for nil.
func jsonText(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v, json.Deterministic(true))
	if err != nil {
		return nil
	}
	return string(b)
}

func fetchAccount(ctx context.Context, c *Connector, _ map[string]string) ([][]any, error) {
	a, err := c.resource(ctx, "/")
	if err != nil {
		return nil, err
	}
	return [][]any{{
		str(a["id"]), str(a["_rid"]),
		members(a["writableLocations"], "name"), members(a["writableLocations"], "databaseAccountEndpoint"),
		members(a["readableLocations"], "name"), members(a["readableLocations"], "databaseAccountEndpoint"),
		str(at(a, "userConsistencyPolicy", "defaultConsistencyLevel")),
		flag(a["enableMultipleWriteLocations"]), flag(a["continuousBackupEnabled"]),
		str(a["queryEngineConfiguration"]),
	}}, nil
}

func fetchDatabases(ctx context.Context, c *Connector, _ map[string]string) ([][]any, error) {
	dbs, err := c.list(ctx, "/dbs", "Databases")
	if err != nil {
		return nil, err
	}
	rows := make([][]any, len(dbs))
	for i, d := range dbs {
		rows[i] = []any{str(d["id"]), str(d["_rid"]), str(d["_self"])}
	}
	return rows, nil
}

// containerRow returns the row of $containers for the definition of a container.
func containerRow(db string, d map[string]any) []any {
	pol := d["indexingPolicy"]
	return []any{
		db, str(d["id"]), str(d["_rid"]), str(d["_self"]),
		array(at(d, "partitionKey", "paths")), str(at(d, "partitionKey", "kind")), whole(at(d, "partitionKey", "version")),
		str(at(pol, "indexingMode")), flag(at(pol, "automatic")),
		members(at(pol, "includedPaths"), "path"), members(at(pol, "excludedPaths"), "path"),
		array(at(pol, "compositeIndexes")), array(at(pol, "spatialIndexes")), array(at(pol, "vectorIndexes")),
		jsonText(pol),
		uniqueKeys(at(d, "uniqueKeyPolicy", "uniqueKeys")),
		whole(d["defaultTtl"]), whole(d["analyticalStorageTtl"]),
		str(at(d, "conflictResolutionPolicy", "mode")), str(at(d, "conflictResolutionPolicy", "conflictResolutionPath")), str(at(d, "conflictResolutionPolicy", "conflictResolutionProcedure")),
		whole(at(d, "changeFeedPolicy", "retentionDuration")),
		array(d["computedProperties"]), mapping(d["vectorEmbeddingPolicy"]), mapping(d["fullTextPolicy"]),
		str(at(d, "geospatialConfig", "type")),
	}
}

func fetchContainers(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db := w["database"]
	if name := w["container"]; name != "" {
		d, err := c.resource(ctx, collURL(db, name))
		if err != nil {
			return nil, err
		}
		return [][]any{containerRow(db, d)}, nil
	}
	list, err := c.list(ctx, dbURL(db)+"/colls", "DocumentCollections")
	if err != nil {
		return nil, err
	}
	rows := make([][]any, len(list))
	for i, d := range list {
		rows[i] = containerRow(db, d)
	}
	return rows, nil
}

// fetchScripts returns the fetch function of the stored procedures, the triggers
// or the functions of a container, which the path and the key of the feed name.
func fetchScripts(path, key string) func(context.Context, *Connector, map[string]string) ([][]any, error) {
	return func(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
		list, err := c.list(ctx, collURL(w["database"], w["container"])+"/"+path, key)
		if err != nil {
			return nil, err
		}
		rows := make([][]any, len(list))
		for i, d := range list {
			row := []any{w["database"], w["container"], str(d["id"]), str(d["_rid"]), str(d["_self"]), str(d["body"])}
			if path == "triggers" {
				row = append(row, str(d["triggerType"]), str(d["triggerOperation"]))
			}
			rows[i] = row
		}
		return rows, nil
	}
}

func fetchUsers(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	list, err := c.list(ctx, dbURL(w["database"])+"/users", "Users")
	if err != nil {
		return nil, err
	}
	rows := make([][]any, len(list))
	for i, d := range list {
		rows[i] = []any{w["database"], str(d["id"]), str(d["_rid"]), str(d["_self"])}
	}
	return rows, nil
}

func fetchPermissions(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db := w["database"]
	var users []string
	if u := w["user"]; u != "" {
		users = []string{u}
	} else {
		list, err := c.list(ctx, dbURL(db)+"/users", "Users")
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			if id, ok := d["id"].(string); ok {
				users = append(users, id)
			}
		}
	}
	rows := [][]any{}
	for _, u := range users {
		list, err := c.list(ctx, dbURL(db)+"/users/"+url.PathEscape(u)+"/permissions", "Permissions")
		if err != nil {
			return nil, err
		}
		for _, d := range list {
			rows = append(rows, []any{db, u, str(d["id"]), str(d["_rid"]), str(d["_self"]), str(d["permissionMode"]), str(d["resource"])})
		}
	}
	return rows, nil
}

// fetchOffers returns the offers of a database and of its containers, or of one
// container. An offer names its resource by the rid of the resource, so the
// statement reads the rid of the database and the rid of each container to
// give the offers their names.
func fetchOffers(ctx context.Context, c *Connector, w map[string]string) ([][]any, error) {
	db, only := w["database"], w["container"]
	d, err := c.resource(ctx, dbURL(db))
	if err != nil {
		return nil, err
	}
	dbRID, _ := d["_rid"].(string)
	colls, err := c.list(ctx, dbURL(db)+"/colls", "DocumentCollections")
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, k := range colls {
		rid, _ := k["_rid"].(string)
		id, _ := k["id"].(string)
		if only == "" || only == id {
			names[rid] = id
		}
	}
	offers, err := c.list(ctx, "/offers", "Offers")
	if err != nil {
		return nil, err
	}
	rows := [][]any{}
	for _, o := range offers {
		rid, _ := o["offerResourceId"].(string)
		var scope, container any = "container", nil
		name, isColl := names[rid]
		switch {
		case rid == dbRID && only == "":
			scope = "database"
		case isColl:
			container = name
		default:
			continue
		}
		auto := at(o, "content", "offerAutopilotSettings")
		var manual, maxRU, inc any
		if auto == nil {
			manual = whole(at(o, "content", "offerThroughput"))
		} else {
			if maxRU = whole(at(auto, "maxThroughput")); maxRU == nil {
				maxRU = whole(at(auto, "maximumTierThroughput"))
			}
			inc = whole(at(auto, "autoUpgradePolicy", "throughputPolicy", "incrementPercent"))
		}
		rows = append(rows, []any{
			scope, db, container, str(o["id"]), str(o["_rid"]), str(o["resource"]), str(o["offerVersion"]), str(o["offerType"]),
			manual, maxRU, inc,
		})
	}
	return rows, nil
}
