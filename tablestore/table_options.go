package tablestore

import (
	"fmt"
	"time"

	"github.com/xo/dbimp"
)

// TableOption sets an option of CreateTable (D109 and D225). The options are the
// settings of the request that a table can have, and a call with none keeps the
// defaults of D225.
type TableOption = dbimp.Option[tableOptions]

// IndexOption sets an option of CreateIndex.
type IndexOption = dbimp.Option[indexOptions]

// DeleteOption sets an option of DeleteTable.
type DeleteOption = dbimp.Option[deleteOptions]

// tableOptions are the options of CreateTable. A zero ttl, deviation and stream
// ask for nothing, and so does a nil allowUpdate.
type tableOptions struct {
	ttl         time.Duration
	maxVersions int64
	read, write int64
	deviation   time.Duration
	allowUpdate *bool
	stream      time.Duration
	localTxn    bool

	// err is the first option that has a value that the call refuses.
	err error
}

// indexOptions are the options of CreateIndex.
type indexOptions struct {
	baseData bool
}

// deleteOptions are the options of DeleteTable.
type deleteOptions struct {
	ifExists bool
}

// invalid records the first option of a table that has a value that the call
// refuses.
func (o *tableOptions) invalid(option string, value any, why string) {
	if o.err == nil {
		o.err = fmt.Errorf("applying the option %s: %v is %s: %w", option, value, why, dbimp.ErrInvalidValue)
	}
}

// WithTimeToLive sets the time after which the server deletes a row. The
// protocol counts whole seconds, so d must be a positive number of them. The
// server accepts a value of one day or more, and refuses a smaller one with
// OTSParameterInvalid. With no option, a row never expires.
func WithTimeToLive(d time.Duration) TableOption {
	return func(o *tableOptions) {
		if d <= 0 || d%time.Second != 0 || d/time.Second > 1<<31-1 {
			o.invalid("WithTimeToLive", d, "not a positive number of whole seconds that fits 32 bits")
			return
		}
		o.ttl = d
	}
}

// WithMaxVersions sets the number of versions of a cell that the table keeps,
// which is 1 with no option. It must be at least 1.
func WithMaxVersions(n int) TableOption {
	return func(o *tableOptions) {
		if n < 1 || n > 1<<31-1 {
			o.invalid("WithMaxVersions", n, "not a number between 1 and 2147483647")
			return
		}
		o.maxVersions = int64(n)
	}
}

// WithReservedThroughput sets the capacity units that the table reserves for
// reads and for writes, which are zero with no option. A value cannot be
// negative. An instance in the mode of capacity that the service bills by use
// refuses a value above zero.
func WithReservedThroughput(read, write int) TableOption {
	return func(o *tableOptions) {
		if read < 0 || write < 0 || read > 1<<31-1 || write > 1<<31-1 {
			o.invalid("WithReservedThroughput", [2]int{read, write}, "not two numbers between 0 and 2147483647")
			return
		}
		o.read, o.write = int64(read), int64(write)
	}
}

// WithDeviation sets how far the version of a written cell can differ from the
// time of the server, which the protocol counts in whole seconds. d must be a
// positive number of them. With no option, the server uses its own default.
func WithDeviation(d time.Duration) TableOption {
	return func(o *tableOptions) {
		if d <= 0 || d%time.Second != 0 {
			o.invalid("WithDeviation", d, "not a positive number of whole seconds")
			return
		}
		o.deviation = d
	}
}

// WithAllowUpdate says whether the table accepts an update of a row. With no
// option, the server uses its own default, which is to accept it.
func WithAllowUpdate(allow bool) TableOption {
	return func(o *tableOptions) { o.allowUpdate = &allow }
}

// WithStream turns on the stream of the table, and sets the time that the server
// keeps a record of it, which the protocol counts in whole hours. d must be a
// positive number of them. With no option, the table has no stream.
func WithStream(d time.Duration) TableOption {
	return func(o *tableOptions) {
		if d <= 0 || d%time.Hour != 0 || d/time.Hour > 1<<31-1 {
			o.invalid("WithStream", d, "not a positive number of whole hours")
			return
		}
		o.stream = d
	}
}

// WithLocalTransaction turns on the local transactions of the table. With no
// option, they are off.
func WithLocalTransaction(enable bool) TableOption {
	return func(o *tableOptions) { o.localTxn = enable }
}

// WithBaseData says whether CreateIndex copies the rows that the table holds into
// the index. With no option it does.
func WithBaseData(include bool) IndexOption {
	return func(o *indexOptions) { o.baseData = include }
}

// WithIfExists makes DeleteTable succeed for a table that is not there, as DROP
// TABLE IF EXISTS does in SQL.
func WithIfExists() DeleteOption {
	return func(o *deleteOptions) { o.ifExists = true }
}

// check returns the error of the first option that has a value that the call
// refuses.
func (o *tableOptions) check() error {
	return o.err
}

// message returns the message TableOptions of the request.
func (o *tableOptions) message() []byte {
	ttl := int64(noExpiry)
	if o.ttl > 0 {
		ttl = int64(o.ttl / time.Second)
	}
	b := appendInt(nil, fieldTimeToLive, ttl)
	b = appendInt(b, fieldMaxVersions, o.maxVersions)
	if o.deviation > 0 {
		b = appendInt(b, fieldDeviation, int64(o.deviation/time.Second))
	}
	if o.allowUpdate != nil {
		var v int64
		if *o.allowUpdate {
			v = 1
		}
		b = appendInt(b, fieldAllowUpdate, v)
	}
	return b
}
