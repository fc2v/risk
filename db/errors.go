package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"

	"github.com/lib/pq"
)

// IsTransient reports whether err is likely to clear on its own, such as an unreachable or overloaded database.
func IsTransient(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}

	if pqErr, ok := errors.AsType[*pq.Error](err); ok {
		switch pqErr.Code.Class() {
		case "08", // connection exception
			"53", // insufficient resources
			"57": // operator intervention, e.g. shutdown or query cancelled
			return true
		}
		return pqErr.Code == "40001" || pqErr.Code == "40P01" // serialization failure, deadlock
	}
	return false
}
