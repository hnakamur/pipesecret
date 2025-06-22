package internal

import (
	"context"
	"fmt"
	"os/exec"
)

type onePasswordItemGetter struct {
	opExePath string
}

func NewOnePasswordItemGetter(opExePath string) (*onePasswordItemGetter, error) {
	if _, err := exec.LookPath(opExePath); err != nil {
		return nil, fmt.Errorf("op exe not found, err=%s", err)
	}
	return &onePasswordItemGetter{
		opExePath: opExePath,
	}, nil
}

func (g *onePasswordItemGetter) GetItem(ctx context.Context, itemName string) (string, error) {
	cmd := exec.CommandContext(ctx, g.opExePath, "item", "get", itemName, "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get item, err=%s", err)
	}
	return string(output), nil
}
