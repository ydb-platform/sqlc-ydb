package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const defaultSQLPackage = "github.com/ydb-platform/ydb-go-sdk/v3"

// options holds plugin_options from the codegen request (JSON).
// Used by all destinations; generatorGoSdk uses SQLPackage, others ignore it.
type options struct {
	Package    string `json:"package"`
	SQLPackage string `json:"sql_package"`
}

func parseOptions(pluginOptions []byte) (*options, error) {
	o := &options{Package: "db", SQLPackage: defaultSQLPackage}
	if len(pluginOptions) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(pluginOptions))
	dec.DisallowUnknownFields()
	if err := dec.Decode(o); err != nil {
		return nil, fmt.Errorf("plugin_options: %w", err)
	}
	if o.Package == "" {
		o.Package = "db"
	}
	if o.SQLPackage == "" {
		o.SQLPackage = defaultSQLPackage
	}
	return o, nil
}
