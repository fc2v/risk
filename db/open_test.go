package db

import (
	"strings"
	"testing"
	"time"
)

func TestOptionsValidate(t *testing.T) {
	const password = "s3cret-pw"
	valid := Options{DSN: "postgres://risk:" + password + "@db:5432/risk?sslmode=verify-full", MaxOpenConns: 25, MaxIdleConns: 25, MaxIdleTime: time.Minute}

	cases := map[string]struct {
		edit    func(o *Options)
		wantErr string
	}{
		"verify-full":        {func(*Options) {}, ""},
		"verify-ca":          {func(o *Options) { o.DSN = strings.Replace(o.DSN, "verify-full", "verify-ca", 1) }, ""},
		"disable":            {func(o *Options) { o.DSN = strings.Replace(o.DSN, "verify-full", "disable", 1) }, ""},
		"postgresql scheme":  {func(o *Options) { o.DSN = strings.Replace(o.DSN, "postgres://", "postgresql://", 1) }, ""},
		"require":            {func(o *Options) { o.DSN = strings.Replace(o.DSN, "verify-full", "require", 1) }, "sslmode must be one of"},
		"no sslmode":         {func(o *Options) { o.DSN = strings.Replace(o.DSN, "?sslmode=verify-full", "", 1) }, "sslmode must be one of"},
		"key=value DSN":      {func(o *Options) { o.DSN = "host=db password=" + password + " sslmode=verify-full" }, "postgres:// URL"},
		"empty":              {func(o *Options) { o.DSN = "" }, "dsn must be set"},
		"no open conns":      {func(o *Options) { o.MaxOpenConns = 0 }, "max open conns"},
		"negative idle time": {func(o *Options) { o.MaxIdleTime = -time.Second }, "max idle time"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o := valid
			tc.edit(&o)
			err := o.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("Validate() = %v, want it to mention %q", err, tc.wantErr)
			case err != nil && strings.Contains(err.Error(), password):
				t.Errorf("error leaks the password: %v", err)
			}
		})
	}
}
