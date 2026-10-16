package web

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"govisualize/analyzer"
	"io"
	"os"
)

type storedGraph struct {
	path string
	size int64
}

type graphWriter struct {
	writer io.Writer
	size   int64
}

func (w *graphWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > maxGraphBytes-w.size {
		return 0, fmt.Errorf("graph exceeds the %d MiB limit", maxGraphBytes>>20)
	}
	n, err := w.writer.Write(data)
	w.size += int64(n)
	return n, err
}

func saveGraph(ctx context.Context, graph analyzer.Graph) (storedGraph, error) {
	file, err := os.CreateTemp("", "govisualize-graph-*.json.gz")
	if err != nil {
		return storedGraph{}, err
	}
	saved := false
	defer func() {
		file.Close()
		if !saved {
			os.Remove(file.Name())
		}
	}()
	compressed, err := gzip.NewWriterLevel(file, gzip.BestSpeed)
	if err != nil {
		return storedGraph{}, err
	}
	writer := &graphWriter{writer: compressed}
	if err := encodeGraph(ctx, writer, graph); err != nil {
		return storedGraph{}, err
	}
	if err := compressed.Close(); err != nil {
		return storedGraph{}, err
	}
	if err := file.Close(); err != nil {
		return storedGraph{}, err
	}
	if err := ctx.Err(); err != nil {
		return storedGraph{}, err
	}
	saved = true
	return storedGraph{path: file.Name(), size: writer.size}, nil
}

func encodeGraph(ctx context.Context, writer io.Writer, graph analyzer.Graph) error {
	if _, err := io.WriteString(writer, "{"); err != nil {
		return err
	}
	fields := []struct {
		name  string
		write func() error
	}{
		{"entrypoints", func() error { return encodeGraphArray(ctx, writer, graph.Entrypoints) }},
		{"functions", func() error { return encodeGraphArray(ctx, writer, graph.Functions) }},
		{"structs", func() error { return encodeGraphArray(ctx, writer, graph.Structs) }},
		{"creations", func() error { return encodeGraphArray(ctx, writer, graph.Creations) }},
		{"edges", func() error { return encodeGraphArray(ctx, writer, graph.Edges) }},
	}
	for i, field := range fields {
		if i > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, "%q:", field.name); err != nil {
			return err
		}
		if err := field.write(); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "}")
	return err
}

func encodeGraphArray[T any](ctx context.Context, writer io.Writer, values []T) error {
	if _, err := io.WriteString(writer, "["); err != nil {
		return err
	}
	encoder := json.NewEncoder(writer)
	for i, value := range values {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i > 0 {
			if _, err := io.WriteString(writer, ","); err != nil {
				return err
			}
		}
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "]")
	return err
}
