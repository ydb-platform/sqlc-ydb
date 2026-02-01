package ydb

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const defaultSQLPackage = "github.com/ydb-platform/ydb-go-sdk/v3"

// Options holds plugin_options from the codegen request (JSON).
type Options struct {
	Package    string `json:"package"`
	SQLPackage string `json:"sql_package"`
}

func parseOptions(pluginOptions []byte) (*Options, error) {
	o := &Options{
		SQLPackage: defaultSQLPackage,
	}
	if len(pluginOptions) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(pluginOptions))
	dec.DisallowUnknownFields()
	if err := dec.Decode(o); err != nil {
		return nil, fmt.Errorf("plugin_options: %w", err)
	}
	if o.SQLPackage == "" {
		o.SQLPackage = defaultSQLPackage
	}
	return o, nil
}
