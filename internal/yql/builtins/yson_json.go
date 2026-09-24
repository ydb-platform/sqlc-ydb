package builtins

import (
	"fmt"

	"github.com/ydb-platform/sqlc-ydb/internal/model"
)

const ysonNodeResource = "Resource<'Yson2.Node'>"

func resolveJsonFrom(name string, args []model.Type) (model.Type, error) {
	if err := arity(name, args, 1); err != nil {
		return model.Type{}, err
	}
	if err := validateYsonCompatible(args[0], true); err != nil {
		return model.Type{}, fmt.Errorf("%s argument 1: %w", name, err)
	}
	return model.Type{Kind: ysonNodeResource}, nil
}
