package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/lib/pq"
)

func TestIsTransient(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"deadline":                 {context.DeadlineExceeded, true},
		"bad conn":                 {driver.ErrBadConn, true},
		"conn done":                {sql.ErrConnDone, true},
		"unexpected eof":           {io.ErrUnexpectedEOF, true},
		"network":                  {&net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		"wrapped network":          {fmt.Errorf("load prior txs: %w", &net.OpError{Op: "read", Err: errors.New("reset")}), true},
		"connection failure 08006": {&pq.Error{Code: "08006"}, true},
		"too many connections":     {&pq.Error{Code: "53300"}, true},
		"admin shutdown":           {&pq.Error{Code: "57P01"}, true},
		"serialization failure":    {&pq.Error{Code: "40001"}, true},
		"deadlock":                 {&pq.Error{Code: "40P01"}, true},
		"unique violation":         {&pq.Error{Code: "23505"}, false},
		"check violation":          {&pq.Error{Code: "23514"}, false},
		"syntax error":             {&pq.Error{Code: "42601"}, false},
		"canceled by caller":       {context.Canceled, false},
		"plain error":              {errors.New("boom"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := IsTransient(tc.err); got != tc.want {
				t.Errorf("IsTransient(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
