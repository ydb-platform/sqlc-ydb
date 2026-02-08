package dbsql

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Options holds plugin_options from the codegen request (JSON).
type Options struct {
	Package string `json:"package"`
}

func parseOptions(pluginOptions []byte) (*Options, error) {
	o := &Options{Package: "db"}
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
	return o, nil
}
