package main

import (
	"context"
	"fmt"
	"io"
)

type environmentRestoreProgressContextKey struct{}

func contextWithEnvironmentRestoreProgress(ctx context.Context, writer io.Writer) context.Context {
	if writer == nil {
		return ctx
	}
	return context.WithValue(ctx, environmentRestoreProgressContextKey{}, writer)
}

func environmentRestoreProgressf(ctx context.Context, format string, args ...any) {
	writer, ok := ctx.Value(environmentRestoreProgressContextKey{}).(io.Writer)
	if !ok || writer == nil {
		return
	}
	if _, err := fmt.Fprintf(writer, format, args...); err != nil {
		return
	}
}
