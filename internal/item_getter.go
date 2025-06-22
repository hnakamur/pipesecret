package internal

import (
	"context"

	"golang.org/x/exp/jsonrpc2"
	errors "golang.org/x/xerrors"
)

type ItemGetter interface {
	GetItem(ctx context.Context, itemName string) (string, error)
}

func GetQueryItem(ctx context.Context, getter ItemGetter, itemName, query string) (string, error) {
	item, err := getter.GetItem(ctx, itemName)
	if err != nil {
		return "", errors.Errorf("%w: %s", jsonrpc2.ErrInvalidRequest, err)
	}
	result, err := runQuery(query, item)
	if err != nil {
		return "", errors.Errorf("%w: %s", jsonrpc2.ErrInvalidRequest, err)
	}
	return result, nil
}
